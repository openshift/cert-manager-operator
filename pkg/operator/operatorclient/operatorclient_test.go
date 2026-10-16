package operatorclient

import (
	"context"
	"encoding/json"
	"fmt"
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
	t.Run("success returns spec, status, and resource version", func(t *testing.T) {
		cm := newCertManager(func(cm *v1alpha1.CertManager) {
			cm.ResourceVersion = "42"
			cm.Spec.ManagementState = operatorv1.Unmanaged
		})
		oc, _ := newTestOperatorClient(t, cm)

		spec, status, rv, err := oc.GetOperatorState()
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if rv != "42" {
			t.Errorf("expected resource version 42, got %s", rv)
		}
		if spec.ManagementState != operatorv1.Unmanaged {
			t.Errorf("expected ManagementState Unmanaged, got %v", spec.ManagementState)
		}
		if status == nil {
			t.Fatal("expected non-nil status")
		}
	})

	t.Run("get error propagates when resource not in lister", func(t *testing.T) {
		oc, _ := newTestOperatorClient(t)

		_, _, _, err := oc.GetOperatorState()
		if err == nil {
			t.Fatal("expected error when no resource exists, got nil")
		}
	})
}

func TestEnsureFinalizer(t *testing.T) {
	ctx := context.Background()

	t.Run("adds finalizer when not present", func(t *testing.T) {
		cm := newCertManager()
		oc, fakeClient := newTestOperatorClient(t, cm)

		if err := oc.EnsureFinalizer(ctx, "test-finalizer"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		updated, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get updated resource: %v", err)
		}
		found := false
		for _, f := range updated.GetFinalizers() {
			if f == "test-finalizer" {
				found = true
				break
			}
		}
		if !found {
			t.Error("expected finalizer 'test-finalizer' to be present on updated resource")
		}
	})

	t.Run("no-op when finalizer already present", func(t *testing.T) {
		cm := newCertManager(func(cm *v1alpha1.CertManager) {
			cm.SetFinalizers([]string{"test-finalizer"})
		})
		oc, fakeClient := newTestOperatorClient(t, cm)

		if err := oc.EnsureFinalizer(ctx, "test-finalizer"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		updated, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get resource: %v", err)
		}
		if updated.ResourceVersion != "1" {
			t.Errorf("expected no update (resource version 1), got %s", updated.ResourceVersion)
		}
	})

	t.Run("save error propagates", func(t *testing.T) {
		cm := newCertManager()
		oc, fakeClient := newTestOperatorClient(t, cm)

		fakeClient.PrependReactor("update", "certmanagers",
			func(action ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("simulated update error")
			})

		err := oc.EnsureFinalizer(ctx, "new-finalizer")
		if err == nil {
			t.Fatal("expected error from save, got nil")
		}
		if err.Error() != "simulated update error" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestRemoveFinalizer(t *testing.T) {
	ctx := context.Background()

	t.Run("removes finalizer when present", func(t *testing.T) {
		cm := newCertManager(func(cm *v1alpha1.CertManager) {
			cm.SetFinalizers([]string{"keep-me", "remove-me", "also-keep"})
		})
		oc, fakeClient := newTestOperatorClient(t, cm)

		if err := oc.RemoveFinalizer(ctx, "remove-me"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		updated, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get updated resource: %v", err)
		}
		for _, f := range updated.GetFinalizers() {
			if f == "remove-me" {
				t.Error("finalizer 'remove-me' should have been removed")
			}
		}
		if len(updated.GetFinalizers()) != 2 {
			t.Errorf("expected 2 remaining finalizers, got %d", len(updated.GetFinalizers()))
		}
	})

	t.Run("no-op when finalizer not present", func(t *testing.T) {
		cm := newCertManager(func(cm *v1alpha1.CertManager) {
			cm.SetFinalizers([]string{"other-finalizer"})
		})
		oc, fakeClient := newTestOperatorClient(t, cm)

		if err := oc.RemoveFinalizer(ctx, "not-present"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		updated, err := fakeClient.OperatorV1alpha1().CertManagers().Get(ctx, "cluster", metav1.GetOptions{})
		if err != nil {
			t.Fatalf("failed to get resource: %v", err)
		}
		if updated.ResourceVersion != "1" {
			t.Errorf("expected no update (resource version 1), got %s", updated.ResourceVersion)
		}
	})

	t.Run("save error propagates", func(t *testing.T) {
		cm := newCertManager(func(cm *v1alpha1.CertManager) {
			cm.SetFinalizers([]string{"test-finalizer"})
		})
		oc, fakeClient := newTestOperatorClient(t, cm)

		fakeClient.PrependReactor("update", "certmanagers",
			func(action ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("simulated update error")
			})

		err := oc.RemoveFinalizer(ctx, "test-finalizer")
		if err == nil {
			t.Fatal("expected error from save, got nil")
		}
		if err.Error() != "simulated update error" {
			t.Errorf("unexpected error message: %v", err)
		}
	})
}

func TestApplyOperatorStatus(t *testing.T) {
	ctx := context.Background()

	t.Run("nil desiredConfiguration returns error", func(t *testing.T) {
		cm := newCertManager()
		oc, _ := newTestOperatorClient(t, cm)

		err := oc.ApplyOperatorStatus(ctx, "test-manager", nil)
		if err == nil {
			t.Fatal("expected error for nil desiredConfiguration, got nil")
		}
		expected := "applyConfiguration must have a value"
		if err.Error() != expected {
			t.Errorf("expected error %q, got %q", expected, err.Error())
		}
	})

	t.Run("not-found error creates new status", func(t *testing.T) {
		// No CertManager resource exists in the fake client.
		oc, _ := newTestOperatorClient(t)

		desired := applyoperatorv1.OperatorStatus()
		err := oc.ApplyOperatorStatus(ctx, "test-manager", desired)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("get error (non-NotFound) propagates", func(t *testing.T) {
		cm := newCertManager()
		oc, fakeClient := newTestOperatorClient(t, cm)

		fakeClient.PrependReactor("get", "certmanagers",
			func(action ktesting.Action) (bool, runtime.Object, error) {
				return true, nil, fmt.Errorf("simulated server error")
			})

		desired := applyoperatorv1.OperatorStatus()
		err := oc.ApplyOperatorStatus(ctx, "test-manager", desired)
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if expected := "unable to get operator configuration: simulated server error"; err.Error() != expected {
			t.Errorf("expected error %q, got %q", expected, err.Error())
		}
	})

	t.Run("deep-equal status skips update", func(t *testing.T) {
		cm := newCertManager()
		oc, fakeClient := newTestOperatorClient(t, cm)

		// Apply once to establish the baseline.
		desired := applyoperatorv1.OperatorStatus()
		if err := oc.ApplyOperatorStatus(ctx, "test-manager", desired); err != nil {
			t.Fatalf("first apply failed: %v", err)
		}

		// Track whether a second apply-status call is made.
		applyStatusCalled := false
		fakeClient.PrependReactor("patch", "certmanagers",
			func(action ktesting.Action) (bool, runtime.Object, error) {
				applyStatusCalled = true
				return false, nil, nil
			})

		// Apply the same status again -- should be a no-op.
		desired2 := applyoperatorv1.OperatorStatus()
		if err := oc.ApplyOperatorStatus(ctx, "test-manager", desired2); err != nil {
			t.Fatalf("second apply failed: %v", err)
		}

		if applyStatusCalled {
			t.Error("expected no apply-status call when status is unchanged")
		}
	})
}
