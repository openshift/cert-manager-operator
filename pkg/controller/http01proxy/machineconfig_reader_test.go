package http01proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

// readerTestManager implements just the manager methods New uses. The embedded
// nil interface makes any other call panic, so a new dependency in New shows
// up here instead of being silently satisfied.
type readerTestManager struct {
	manager.Manager

	cachedClient client.Client
	apiReader    client.Reader
}

func (m readerTestManager) GetClient() client.Client    { return m.cachedClient }
func (m readerTestManager) GetAPIReader() client.Reader { return m.apiReader }
func (m readerTestManager) GetEventRecorderFor(string) record.EventRecorder {
	return record.NewFakeRecorder(1)
}

// sentinelReader is a distinct client.Reader so New's wiring can be checked by
// identity.
type sentinelReader struct {
	client.Reader
}

func TestNewWiresUncachedMachineConfigReader(t *testing.T) {
	reader := &sentinelReader{}
	r, err := New(readerTestManager{apiReader: reader})
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if r.mcReader == nil {
		t.Fatal("New() must set mcReader; a nil reader panics on the first MachineConfig Get")
	}
	if r.mcReader != client.Reader(reader) {
		t.Errorf("mcReader = %T(%p), want the manager's API reader %p", r.mcReader, r.mcReader, reader)
	}
}

// failOnCachedMachineConfigGet makes the cached client reject MachineConfig
// reads. A cached Get would start a cluster-wide MachineConfig informer, which
// needs list/watch RBAC the operator is no longer granted.
func failOnCachedMachineConfigGet(t *testing.T, cached *fakes.FakeCtrlClient) {
	t.Helper()
	next := cached.GetStub
	cached.GetCalls(func(ctx context.Context, key types.NamespacedName, obj client.Object) error {
		if _, ok := obj.(*mcfgv1.MachineConfig); ok {
			t.Errorf("MachineConfig %q was read through the cached client; it must use mcReader", key.Name)
			return fmt.Errorf("unexpected cached MachineConfig Get")
		}
		if next == nil {
			return nil
		}
		return next(ctx, key, obj)
	})
}

func TestCreateOrApplyMachineConfigReadsThroughMCReader(t *testing.T) {
	current, err := renderMachineConfig(vipFamilies(t, "10.0.0.253", "10.0.0.2"))
	if err != nil {
		t.Fatalf("renderMachineConfig() error: %v", err)
	}
	stale := current.DeepCopy()
	stale.SetResourceVersion("99")
	metav1.SetMetaDataAnnotation(&stale.ObjectMeta, machineConfigFingerprintAnnotation, "stale")

	tests := []struct {
		name       string
		existing   *mcfgv1.MachineConfig // nil means NotFound
		readerErr  error
		wantErr    string
		wantCreate int
		wantUpdate int
	}{
		{name: "missing MachineConfig is created", wantCreate: 1},
		{name: "matching fingerprint is left alone", existing: current},
		{name: "stale fingerprint is updated", existing: stale, wantUpdate: 1},
		{name: "reader error is returned", readerErr: fmt.Errorf("forbidden"), wantErr: "failed to get MachineConfig"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cached := &fakes.FakeCtrlClient{}
			failOnCachedMachineConfigGet(t, cached)

			reader := &fakes.FakeCtrlClient{}
			reader.GetCalls(func(_ context.Context, key types.NamespacedName, obj client.Object) error {
				if key.Name != machineConfigName {
					t.Errorf("mcReader Get for %q, want only %q (RBAC get is scoped to that name)", key.Name, machineConfigName)
				}
				switch {
				case tt.readerErr != nil:
					return tt.readerErr
				case tt.existing == nil:
					return errors.NewNotFound(machineConfigGR, machineConfigName)
				default:
					tt.existing.DeepCopyInto(obj.(*mcfgv1.MachineConfig))
					return nil
				}
			})

			r := newTestReconciler(cached)
			r.mcReader = readerFor(reader)

			err := r.createOrApplyMachineConfig(context.Background(), vipInfo("10.0.0.253", "10.0.0.2"))
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want substring %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if reader.GetCallCount() != 1 {
				t.Errorf("mcReader Get calls = %d, want 1", reader.GetCallCount())
			}
			if cached.CreateCallCount() != tt.wantCreate {
				t.Errorf("Create calls = %d, want %d", cached.CreateCallCount(), tt.wantCreate)
			}
			if cached.UpdateCallCount() != tt.wantUpdate {
				t.Errorf("Update calls = %d, want %d", cached.UpdateCallCount(), tt.wantUpdate)
			}
			if tt.wantUpdate > 0 {
				_, obj, _ := cached.UpdateArgsForCall(0)
				if obj.GetName() != machineConfigName {
					t.Errorf("updated %q, want %q (RBAC update is scoped to that name)", obj.GetName(), machineConfigName)
				}
				if obj.GetResourceVersion() != stale.GetResourceVersion() {
					t.Errorf("Update resourceVersion = %q, want %q from mcReader", obj.GetResourceVersion(), stale.GetResourceVersion())
				}
			}
		})
	}
}

// TestReconcileNeverReadsMachineConfigFromCache runs a full reconcile and
// checks that the HTTP01Proxy and Infrastructure come from the cached client
// while the MachineConfig comes only from mcReader.
func TestReconcileNeverReadsMachineConfigFromCache(t *testing.T) {
	cached := &fakes.FakeCtrlClient{}
	stubReconcileGets(cached, []string{"10.0.0.254"}, []string{"10.0.0.1"}, nil)
	failOnCachedMachineConfigGet(t, cached)

	reader := &fakes.FakeCtrlClient{}
	reader.GetReturns(errors.NewNotFound(machineConfigGR, machineConfigName))

	r := newTestReconciler(cached)
	r.mcReader = readerFor(reader)

	if _, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
	}); err != nil {
		t.Fatalf("Reconcile() error: %v", err)
	}

	if reader.GetCallCount() != 1 {
		t.Errorf("mcReader Get calls = %d, want 1", reader.GetCallCount())
	}
	if cached.CreateCallCount() != 1 {
		t.Fatalf("Create calls = %d, want 1", cached.CreateCallCount())
	}
	if ready := lastStatusCondition(t, cached, v1alpha1.Ready); ready == nil || ready.Status != metav1.ConditionTrue {
		t.Errorf("Ready condition = %+v, want status True", ready)
	}
}
