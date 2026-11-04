package http01proxy

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"

	"github.com/go-logr/logr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	mcfgv1 "github.com/openshift/api/machineconfiguration/v1"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func newTestReconciler(client *fakes.FakeCtrlClient) *Reconciler {
	return &Reconciler{
		CtrlClient:    client,
		eventRecorder: record.NewFakeRecorder(10),
		log:           logr.Discard(),
	}
}

func TestReconcileNotFound(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(errors.NewNotFound(schema.GroupResource{Group: "operator.openshift.io", Resource: "http01proxies"}, http01proxyObjectName))

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestReconcileGetError(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.GetReturns(fmt.Errorf("api server unavailable"))

	r := newTestReconciler(fakeClient)

	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
	})

	if err == nil {
		t.Fatal("expected error for Get failure")
	}
	if !strings.Contains(err.Error(), "failed to fetch") {
		t.Errorf("error = %q, want substring %q", err.Error(), "failed to fetch")
	}
}

func TestReconcileDeletion(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	now := metav1.Now()
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		proxy := obj.(*v1alpha1.HTTP01Proxy)
		proxy.Name = http01proxyObjectName
		proxy.DeletionTimestamp = &now
		proxy.Finalizers = []string{finalizer}
		return nil
	}
	fakeClient.DeleteReturns(nil)
	fakeClient.UpdateWithRetryReturns(nil)

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result after deletion, got %v", result)
	}
	if fakeClient.DeleteCallCount() == 0 {
		t.Error("expected Delete to be called during cleanup")
	}
}

func TestCleanUpDeletesMachineConfig(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(nil)

	r := newTestReconciler(fakeClient)
	proxy := &v1alpha1.HTTP01Proxy{}
	proxy.SetName(http01proxyObjectName)

	err := r.cleanUp(context.Background(), proxy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if fakeClient.DeleteCallCount() != 1 {
		t.Errorf("expected 1 Delete call (MachineConfig), got %d", fakeClient.DeleteCallCount())
	}
}

func TestCleanUpMachineConfigDeleteFails(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturnsOnCall(0, fmt.Errorf("mc delete failed"))

	r := newTestReconciler(fakeClient)
	proxy := &v1alpha1.HTTP01Proxy{}
	proxy.SetName(http01proxyObjectName)

	err := r.cleanUp(context.Background(), proxy)
	if err == nil {
		t.Fatal("expected error when MachineConfig delete fails")
	}
	if !strings.Contains(err.Error(), "MachineConfig") {
		t.Errorf("error = %q, want substring %q", err.Error(), "MachineConfig")
	}
}

func TestCleanUpMachineConfigAlreadyGone(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	fakeClient.DeleteReturns(errors.NewNotFound(
		schema.GroupResource{Group: "machineconfiguration.openshift.io", Resource: "machineconfigs"},
		machineConfigName,
	))

	r := newTestReconciler(fakeClient)
	proxy := &v1alpha1.HTTP01Proxy{}
	proxy.SetName(http01proxyObjectName)

	err := r.cleanUp(context.Background(), proxy)
	if err != nil {
		t.Fatalf("cleanUp should succeed when MachineConfig is already gone, got: %v", err)
	}
}

func TestReconcileDeletionWithoutFinalizer(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	now := metav1.Now()
	fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
		proxy := obj.(*v1alpha1.HTTP01Proxy)
		proxy.Name = http01proxyObjectName
		proxy.DeletionTimestamp = &now
		proxy.Finalizers = []string{}
		return nil
	}

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result, got %v", result)
	}
}

// stubReconcileGets serves Get calls for a full Reconcile: the HTTP01Proxy
// (prepared by proxyFn), a BareMetal Infrastructure with the given VIPs, and
// a MachineConfig that does not exist yet.
func stubReconcileGets(fakeClient *fakes.FakeCtrlClient, apiVIPs, ingressVIPs []string, proxyFn func(*v1alpha1.HTTP01Proxy)) {
	fakeClient.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
		switch o := obj.(type) {
		case *v1alpha1.HTTP01Proxy:
			o.SetName(http01proxyObjectName)
			if proxyFn != nil {
				proxyFn(o)
			}
			return nil
		case *configv1.Infrastructure:
			o.SetName("cluster")
			o.Status.PlatformStatus = &configv1.PlatformStatus{
				Type: configv1.BareMetalPlatformType,
				BareMetal: &configv1.BareMetalPlatformStatus{
					APIServerInternalIPs: apiVIPs,
					IngressIPs:           ingressVIPs,
				},
			}
			return nil
		case *mcfgv1.MachineConfig:
			return errors.NewNotFound(machineConfigGR, machineConfigName)
		default:
			return fmt.Errorf("unexpected Get for %T", obj)
		}
	})
}

// lastStatusCondition returns the given condition from the most recent
// status update, or nil when the status was never updated.
func lastStatusCondition(t *testing.T, fakeClient *fakes.FakeCtrlClient, condType string) *metav1.Condition {
	t.Helper()
	n := fakeClient.StatusUpdateCallCount()
	if n == 0 {
		return nil
	}
	_, obj, _ := fakeClient.StatusUpdateArgsForCall(n - 1)
	return obj.(*v1alpha1.HTTP01Proxy).Status.GetCondition(condType)
}

func TestReconcileCreatesMachineConfigAndSetsReady(t *testing.T) {
	tests := []struct {
		name        string
		apiVIPs     []string
		ingressVIPs []string
		wantNFT     []string
		unwantedNFT []string
	}{
		{
			name:        "IPv4 only",
			apiVIPs:     []string{"10.0.0.254"},
			ingressVIPs: []string{"10.0.0.1"},
			wantNFT:     []string{"dnat ip to 10.0.0.1:80"},
			unwantedNFT: []string{"ip6 "},
		},
		{
			name:        "IPv6 only",
			apiVIPs:     []string{"fd00::254"},
			ingressVIPs: []string{"fd00::1"},
			wantNFT:     []string{"dnat ip6 to [fd00::1]:80"},
			unwantedNFT: []string{"ip daddr"},
		},
		{
			name:        "IPv6 only with global VIPs",
			apiVIPs:     []string{"2620:52:0:2e61::1632"},
			ingressVIPs: []string{"2620:52:0:2e61::1648"},
			wantNFT:     []string{"ip6 daddr 2620:52:0:2e61::1632 tcp dport 80 dnat ip6 to [2620:52:0:2e61::1648]:80"},
			unwantedNFT: []string{"ip daddr"},
		},
		{
			name:        "IPv6 only in API VIPs renders IPv4 only",
			apiVIPs:     []string{"10.0.0.254", "fd00::254"},
			ingressVIPs: []string{"10.0.0.1"},
			wantNFT:     []string{"dnat ip to 10.0.0.1:80"},
			unwantedNFT: []string{"ip6 "},
		},
		{
			name:        "IPv4 only in ingress VIPs renders IPv6 only",
			apiVIPs:     []string{"fd00::254"},
			ingressVIPs: []string{"10.0.0.1", "fd00::1"},
			wantNFT:     []string{"dnat ip6 to [fd00::1]:80"},
			unwantedNFT: []string{"ip daddr"},
		},
		{
			name:        "IPv4-mapped IPv6 API VIP renders as IPv4",
			apiVIPs:     []string{"::ffff:10.0.0.254"},
			ingressVIPs: []string{"10.0.0.1"},
			wantNFT:     []string{"ip daddr 10.0.0.254 tcp dport 80 dnat ip to 10.0.0.1:80"},
			unwantedNFT: []string{"ip6 ", "::ffff"},
		},
		{
			name:        "non-canonical IPv6 renders canonical",
			apiVIPs:     []string{"FD00:0::254"},
			ingressVIPs: []string{"fd00:0:0:0:0:0:0:1"},
			wantNFT:     []string{"ip6 daddr fd00::254 tcp dport 80 dnat ip6 to [fd00::1]:80"},
		},
		{
			name:        "dual-stack",
			apiVIPs:     []string{"fd00::254", "10.0.0.254"},
			ingressVIPs: []string{"fd00::1", "10.0.0.1"},
			wantNFT:     []string{"dnat ip to 10.0.0.1:80", "dnat ip6 to [fd00::1]:80"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			stubReconcileGets(fakeClient, tt.apiVIPs, tt.ingressVIPs, nil)
			// Record finalizers at call time: addFinalizer later overwrites the same
			// object with the re-fetched copy.
			var updatedFinalizers [][]string
			fakeClient.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
				updatedFinalizers = append(updatedFinalizers, append([]string(nil), obj.GetFinalizers()...))
				return nil
			})

			r := newTestReconciler(fakeClient)

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != (ctrl.Result{}) {
				t.Errorf("expected empty result, got %v", result)
			}

			if len(updatedFinalizers) == 0 || !slices.Contains(updatedFinalizers[0], finalizer) {
				t.Errorf("first UpdateWithRetry should add finalizer, got %v", updatedFinalizers)
			}

			if fakeClient.CreateCallCount() != 1 {
				t.Fatalf("expected 1 Create call, got %d", fakeClient.CreateCallCount())
			}
			_, obj, _ := fakeClient.CreateArgsForCall(0)
			nftRules := extractNFTRules(t, obj.(*mcfgv1.MachineConfig))
			for _, want := range tt.wantNFT {
				if !strings.Contains(nftRules, want) {
					t.Errorf("created MachineConfig nftables rules missing %q:\n%s", want, nftRules)
				}
			}
			for _, unwanted := range tt.unwantedNFT {
				if strings.Contains(nftRules, unwanted) {
					t.Errorf("created MachineConfig nftables rules should not contain %q:\n%s", unwanted, nftRules)
				}
			}

			ready := lastStatusCondition(t, fakeClient, v1alpha1.Ready)
			if ready == nil || ready.Status != metav1.ConditionTrue {
				t.Errorf("Ready condition = %+v, want status True", ready)
			}
		})
	}
}

// Validation failures are irrecoverable: no requeue, the MachineConfig is
// removed and the CR is marked Degraded.
func TestReconcileInvalidVIPsAreIrrecoverable(t *testing.T) {
	tests := []struct {
		name        string
		apiVIPs     []string
		ingressVIPs []string
		wantMsg     string
	}{
		{
			name:        "IPv4 API VIP and IPv6 ingress VIP",
			apiVIPs:     []string{"10.0.0.254"},
			ingressVIPs: []string{"fd00::1"},
			wantMsg:     "share no IP family",
		},
		{
			name:        "IPv6 API VIP and IPv4 ingress VIP",
			apiVIPs:     []string{"fd00::254"},
			ingressVIPs: []string{"10.0.0.1"},
			wantMsg:     "share no IP family",
		},
		{
			name:        "unparseable IPv4 VIP",
			apiVIPs:     []string{"10.0.0.300"},
			ingressVIPs: []string{"10.0.0.1"},
			wantMsg:     "invalid API VIP",
		},
		{
			name:        "zoned IPv6 VIP",
			apiVIPs:     []string{"fe80::1%br-ex"},
			ingressVIPs: []string{"fe80::2"},
			wantMsg:     "IPv6 zone",
		},
		{
			name:        "same IPv6 VIP in different notation",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"FD00:0::1"},
			wantMsg:     "are the same",
		},
		{
			name:        "same IPv4 VIP for API and ingress",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"10.0.0.1"},
			wantMsg:     "are the same",
		},
		{
			name:        "same IPv6 VIP in dual-stack",
			apiVIPs:     []string{"10.0.0.254", "fd00::1"},
			ingressVIPs: []string{"10.0.0.1", "fd00::1"},
			wantMsg:     "are the same",
		},
		{
			name:        "IPv6 VIP with CIDR suffix",
			apiVIPs:     []string{"fd00::254/128"},
			ingressVIPs: []string{"fd00::1"},
			wantMsg:     "invalid API VIP",
		},
		{
			name:        "IPv4 VIP with CIDR suffix",
			apiVIPs:     []string{"10.0.0.254"},
			ingressVIPs: []string{"10.0.0.1/32"},
			wantMsg:     "invalid ingress VIP",
		},
		{
			name:        "no API VIPs",
			apiVIPs:     nil,
			ingressVIPs: []string{"fd00::1"},
			wantMsg:     "no API server VIPs",
		},
		{
			name:        "no ingress VIPs",
			apiVIPs:     []string{"fd00::254"},
			ingressVIPs: nil,
			wantMsg:     "no ingress VIPs",
		},
		{
			name:        "unparseable IPv6 VIP",
			apiVIPs:     []string{"fd00::254"},
			ingressVIPs: []string{"fd00::zz"},
			wantMsg:     "invalid ingress VIP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			stubReconcileGets(fakeClient, tt.apiVIPs, tt.ingressVIPs, func(p *v1alpha1.HTTP01Proxy) {
				p.Finalizers = []string{finalizer}
			})

			r := newTestReconciler(fakeClient)

			result, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result != (ctrl.Result{}) {
				t.Errorf("irrecoverable error should not requeue, got %v", result)
			}
			if fakeClient.CreateCallCount() != 0 {
				t.Errorf("expected no MachineConfig Create, got %d", fakeClient.CreateCallCount())
			}
			if fakeClient.DeleteCallCount() != 1 {
				t.Errorf("expected cleanup to delete the MachineConfig once, got %d Delete calls", fakeClient.DeleteCallCount())
			}

			degraded := lastStatusCondition(t, fakeClient, v1alpha1.Degraded)
			if degraded == nil || degraded.Status != metav1.ConditionTrue {
				t.Fatalf("Degraded condition = %+v, want status True", degraded)
			}
			if !strings.Contains(degraded.Message, tt.wantMsg) {
				t.Errorf("Degraded message = %q, want substring %q", degraded.Message, tt.wantMsg)
			}
		})
	}
}

// A recoverable error is returned to controller-runtime (rate-limited
// requeue) after the status is set to Ready=False/InProgress.
func TestReconcileMachineConfigCreateErrorIsRetried(t *testing.T) {
	fakeClient := &fakes.FakeCtrlClient{}
	stubReconcileGets(fakeClient, []string{"10.0.0.254"}, []string{"10.0.0.1"}, func(p *v1alpha1.HTTP01Proxy) {
		p.Finalizers = []string{finalizer}
	})
	fakeClient.CreateReturns(fmt.Errorf("admission webhook unavailable"))

	r := newTestReconciler(fakeClient)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
	})
	if err == nil || !strings.Contains(err.Error(), "admission webhook unavailable") {
		t.Fatalf("Reconcile() error = %v, want the MachineConfig create error", err)
	}
	if result != (ctrl.Result{}) {
		t.Errorf("expected empty result alongside error, got %v", result)
	}

	ready := lastStatusCondition(t, fakeClient, v1alpha1.Ready)
	if ready == nil || ready.Status != metav1.ConditionFalse || ready.Reason != v1alpha1.ReasonInProgress {
		t.Errorf("Ready condition = %+v, want status False with reason %q", ready, v1alpha1.ReasonInProgress)
	}
}

func TestReconcileErrorPaths(t *testing.T) {
	now := metav1.Now()
	deleting := func(p *v1alpha1.HTTP01Proxy) {
		p.DeletionTimestamp = &now
		p.Finalizers = []string{finalizer}
	}

	tests := []struct {
		name    string
		proxyFn func(*v1alpha1.HTTP01Proxy)
		setup   func(*fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name:    "deletion cleanup fails",
			proxyFn: deleting,
			setup: func(f *fakes.FakeCtrlClient) {
				f.DeleteReturns(fmt.Errorf("api server unavailable"))
			},
			wantErr: "clean up failed",
		},
		{
			name:    "deletion finalizer removal fails",
			proxyFn: deleting,
			setup: func(f *fakes.FakeCtrlClient) {
				f.UpdateWithRetryReturns(fmt.Errorf("conflict"))
			},
			wantErr: "failed to remove finalizers",
		},
		{
			name: "adding finalizer fails",
			setup: func(f *fakes.FakeCtrlClient) {
				f.UpdateWithRetryReturns(fmt.Errorf("conflict"))
			},
			wantErr: "with finalizers",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			stubReconcileGets(fakeClient, []string{"10.0.0.254"}, []string{"10.0.0.1"}, tt.proxyFn)
			tt.setup(fakeClient)

			r := newTestReconciler(fakeClient)

			_, err := r.Reconcile(context.Background(), ctrl.Request{
				NamespacedName: types.NamespacedName{Name: http01proxyObjectName},
			})
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("Reconcile() error = %v, want substring %q", err, tt.wantErr)
			}
			if fakeClient.CreateCallCount() != 0 {
				t.Errorf("expected no MachineConfig Create, got %d", fakeClient.CreateCallCount())
			}
		})
	}
}
