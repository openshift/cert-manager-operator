package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	applyoperatorv1 "github.com/openshift/client-go/operator/applyconfigurations/operator/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
	"k8s.io/utils/clock"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	fakeclientset "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned/fake"
	informers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
)

func newTestOperatorClient(t *testing.T, objects ...runtime.Object) (OperatorClient, *fakeclientset.Clientset) {
	t.Helper()

	fakeClient := fakeclientset.NewClientset(objects...)
	factory := informers.NewSharedInformerFactory(fakeClient, 0)

	informer := factory.Operator().V1alpha1().CertManagers().Informer()
	for _, obj := range objects {
		if err := informer.GetIndexer().Add(obj); err != nil {
			t.Fatalf("failed to add object to indexer: %v", err)
		}
	}

	oc := OperatorClient{
		Informers: factory,
		Client:    fakeClient.OperatorV1alpha1(),
		Clock:     clock.RealClock{},
	}
	return oc, fakeClient
}

func newCertManager(opts ...func(*v1alpha1.CertManager)) *v1alpha1.CertManager {
	cm := &v1alpha1.CertManager{
		TypeMeta: metav1.TypeMeta{
			Kind:       "CertManager",
			APIVersion: "operator.openshift.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:            "cluster",
			ResourceVersion: "1",
		},
		Spec: v1alpha1.CertManagerSpec{
			OperatorSpec: operatorv1.OperatorSpec{
				ManagementState: operatorv1.Managed,
			},
		},
	}
	for _, fn := range opts {
		fn(cm)
	}
	return cm
}

func TestGetUnsupportedConfigOverrides(t *testing.T) {
	tests := []struct {
		name        string
		rawBytes    []byte
		expectNil   bool
		expectErr   bool
		expectValue *v1alpha1.UnsupportedConfigOverrides
	}{
		{
			name:      "empty raw bytes returns nil config",
			rawBytes:  nil,
			expectNil: true,
		},
		{
			name:     "valid JSON returns parsed UnsupportedConfigOverrides",
			rawBytes: []byte(`{"controller":{"args":["--foo","--bar"]}}`),
			expectValue: &v1alpha1.UnsupportedConfigOverrides{
				Controller: v1alpha1.UnsupportedConfigOverridesForCertManagerController{
					Args: []string{"--foo", "--bar"},
				},
			},
		},
		{
			name:      "invalid JSON returns unmarshal error",
			rawBytes:  []byte(`{not-valid-json`),
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := &operatorv1.OperatorSpec{
				UnsupportedConfigOverrides: runtime.RawExtension{
					Raw: tc.rawBytes,
				},
			}
			result, err := GetUnsupportedConfigOverrides(spec)

			if tc.expectErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tc.expectNil {
				if result != nil {
					t.Fatalf("expected nil result, got %+v", result)
				}
				return
			}

			got, _ := json.Marshal(result)
			want, _ := json.Marshal(tc.expectValue)
			if string(got) != string(want) {
				t.Errorf("expected %s, got %s", want, got)
			}
		})
	}
}

func TestGetOperatorState(t *testing.T) {
	tests := []struct {
		name      string
		objects   []runtime.Object
		wantErr   bool
		wantRV    string
		wantState operatorv1.ManagementState
	}{
		{
			name: "success returns spec, status, and resource version",
			objects: []runtime.Object{newCertManager(func(cm *v1alpha1.CertManager) {
				cm.ResourceVersion = "42"
				cm.Spec.ManagementState = operatorv1.Unmanaged
			})},
			wantRV:    "42",
			wantState: operatorv1.Unmanaged,
		},
		{
			name:    "get error propagates when resource not in lister",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oc, _ := newTestOperatorClient(t, tt.objects...)

			spec, status, rv, err := oc.GetOperatorState()
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rv != tt.wantRV {
				t.Errorf("expected resource version %s, got %s", tt.wantRV, rv)
			}
			if spec.ManagementState != tt.wantState {
				t.Errorf("expected ManagementState %v, got %v", tt.wantState, spec.ManagementState)
			}
			if status == nil {
				t.Fatal("expected non-nil status")
			}
		})
	}
}

// finalizerTestCase describes a single EnsureFinalizer or RemoveFinalizer case.
type finalizerTestCase struct {
	name              string
	initialFinalizers []string
	finalizer         string
	updateErr         error
	wantErr           string
	wantFinalizers    []string
	wantNoUpdate      bool
}

func runFinalizerTests(t *testing.T, tests []finalizerTestCase, op func(OperatorClient, context.Context, string) error) {
	t.Helper()
	ctx := context.Background()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := newCertManager(func(cm *v1alpha1.CertManager) {
				cm.SetFinalizers(tt.initialFinalizers)
			})
			oc, fakeClient := newTestOperatorClient(t, cm)
			if tt.updateErr != nil {
				fakeClient.PrependReactor("update", "certmanagers",
					func(action ktesting.Action) (bool, runtime.Object, error) {
						return true, nil, tt.updateErr
					})
			}

			err := op(oc, ctx, tt.finalizer)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error from save, got nil")
				}
				if err.Error() != tt.wantErr {
					t.Errorf("unexpected error message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			updated, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get resource: %v", err)
			}
			if tt.wantNoUpdate && updated.ResourceVersion != "1" {
				t.Errorf("expected no update (resource version 1), got %s", updated.ResourceVersion)
			}
			if !slices.Equal(updated.GetFinalizers(), tt.wantFinalizers) {
				t.Errorf("expected finalizers %v, got %v", tt.wantFinalizers, updated.GetFinalizers())
			}
		})
	}
}

func TestEnsureFinalizer(t *testing.T) {
	runFinalizerTests(t, []finalizerTestCase{
		{
			name:           "adds finalizer when not present",
			finalizer:      "test-finalizer",
			wantFinalizers: []string{"test-finalizer"},
		},
		{
			name:              "no-op when finalizer already present",
			initialFinalizers: []string{"test-finalizer"},
			finalizer:         "test-finalizer",
			wantFinalizers:    []string{"test-finalizer"},
			wantNoUpdate:      true,
		},
		{
			name:      "save error propagates",
			finalizer: "new-finalizer",
			updateErr: fmt.Errorf("simulated update error"),
			wantErr:   "simulated update error",
		},
	}, OperatorClient.EnsureFinalizer)
}

func TestRemoveFinalizer(t *testing.T) {
	runFinalizerTests(t, []finalizerTestCase{
		{
			name:              "removes finalizer when present",
			initialFinalizers: []string{"keep-me", "remove-me", "also-keep"},
			finalizer:         "remove-me",
			wantFinalizers:    []string{"keep-me", "also-keep"},
		},
		{
			name:              "no-op when finalizer not present",
			initialFinalizers: []string{"other-finalizer"},
			finalizer:         "not-present",
			wantFinalizers:    []string{"other-finalizer"},
			wantNoUpdate:      true,
		},
		{
			name:              "save error propagates",
			initialFinalizers: []string{"test-finalizer"},
			finalizer:         "test-finalizer",
			updateErr:         fmt.Errorf("simulated update error"),
			wantErr:           "simulated update error",
		},
	}, OperatorClient.RemoveFinalizer)
}

func TestApplyOperatorStatus(t *testing.T) {
	tests := []struct {
		name    string
		objects []runtime.Object
		desired *applyoperatorv1.OperatorStatusApplyConfiguration
		getErr  error
		// applyTwice applies desired once as a baseline, then asserts that
		// applying it again does not patch the resource.
		applyTwice bool
		wantErr    string
	}{
		{
			name:    "nil desiredConfiguration returns error",
			objects: []runtime.Object{newCertManager()},
			wantErr: "applyConfiguration must have a value",
		},
		{
			name:    "not-found error creates new status",
			desired: applyoperatorv1.OperatorStatus(),
		},
		{
			name:    "get error (non-NotFound) propagates",
			objects: []runtime.Object{newCertManager()},
			desired: applyoperatorv1.OperatorStatus(),
			getErr:  fmt.Errorf("simulated server error"),
			wantErr: "unable to get operator configuration: simulated server error",
		},
		{
			name:       "deep-equal status skips update",
			objects:    []runtime.Object{newCertManager()},
			desired:    applyoperatorv1.OperatorStatus(),
			applyTwice: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			oc, fakeClient := newTestOperatorClient(t, tt.objects...)
			if tt.getErr != nil {
				fakeClient.PrependReactor("get", "certmanagers",
					func(action ktesting.Action) (bool, runtime.Object, error) {
						return true, nil, tt.getErr
					})
			}

			err := oc.ApplyOperatorStatus(ctx, "test-manager", tt.desired)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tt.wantErr {
					t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.applyTwice {
				return
			}

			applyStatusCalled := false
			fakeClient.PrependReactor("patch", "certmanagers",
				func(action ktesting.Action) (bool, runtime.Object, error) {
					applyStatusCalled = true
					return false, nil, nil
				})
			if err := oc.ApplyOperatorStatus(ctx, "test-manager", applyoperatorv1.OperatorStatus()); err != nil {
				t.Fatalf("second apply failed: %v", err)
			}
			if applyStatusCalled {
				t.Error("expected no apply-status call when status is unchanged")
			}
		})
	}
}
