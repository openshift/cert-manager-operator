package trustmanager

import (
	"context"
	"testing"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestUpdateStatusObservedState(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)

	tests := []struct {
		name             string
		trustManager     func() *v1alpha1.TrustManager
		wantStatusUpdate int
		assertStatus     func(*testing.T, *v1alpha1.TrustManager)
	}{
		{
			name: "updates all observed fields when status is empty",
			trustManager: func() *v1alpha1.TrustManager {
				return testTrustManager().Build()
			},
			wantStatusUpdate: 1,
			assertStatus: func(t *testing.T, tm *v1alpha1.TrustManager) {
				s := tm.Status
				if s.TrustManagerImage != testImage {
					t.Errorf("TrustManagerImage: got %q, want %q", s.TrustManagerImage, testImage)
				}
				if s.TrustNamespace != defaultTrustNamespace {
					t.Errorf("TrustNamespace: got %q, want %q", s.TrustNamespace, defaultTrustNamespace)
				}
				if s.SecretTargetsPolicy != tm.Spec.TrustManagerConfig.SecretTargets.Policy {
					t.Errorf("SecretTargetsPolicy: got %q, want %q", s.SecretTargetsPolicy, tm.Spec.TrustManagerConfig.SecretTargets.Policy)
				}
				if s.DefaultCAPackagePolicy != tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy {
					t.Errorf("DefaultCAPackagePolicy: got %q, want %q", s.DefaultCAPackagePolicy, tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy)
				}
				if s.FilterExpiredCertificatesPolicy != tm.Spec.TrustManagerConfig.FilterExpiredCertificates {
					t.Errorf("FilterExpiredCertificatesPolicy: got %q, want %q", s.FilterExpiredCertificatesPolicy, tm.Spec.TrustManagerConfig.FilterExpiredCertificates)
				}
			},
		},
		{
			name: "updates all observed fields for custom spec",
			trustManager: func() *v1alpha1.TrustManager {
				tm := testTrustManager().WithTrustNamespace("custom-trust-ns").Build()
				tm.Spec.TrustManagerConfig.SecretTargets.Policy = v1alpha1.SecretTargetsPolicyCustom
				tm.Spec.TrustManagerConfig.SecretTargets.AuthorizedSecrets = []string{"allowed-secret"}
				tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy = v1alpha1.DefaultCAPackagePolicyEnabled
				tm.Spec.TrustManagerConfig.FilterExpiredCertificates = v1alpha1.FilterExpiredCertificatesPolicyEnabled
				return tm
			},
			wantStatusUpdate: 1,
			assertStatus: func(t *testing.T, tm *v1alpha1.TrustManager) {
				s := tm.Status
				if s.TrustManagerImage != testImage {
					t.Errorf("TrustManagerImage: got %q, want %q", s.TrustManagerImage, testImage)
				}
				if s.TrustNamespace != "custom-trust-ns" {
					t.Errorf("TrustNamespace: got %q, want %q", s.TrustNamespace, "custom-trust-ns")
				}
				if s.SecretTargetsPolicy != v1alpha1.SecretTargetsPolicyCustom {
					t.Errorf("SecretTargetsPolicy: got %q, want %q", s.SecretTargetsPolicy, v1alpha1.SecretTargetsPolicyCustom)
				}
				if s.DefaultCAPackagePolicy != v1alpha1.DefaultCAPackagePolicyEnabled {
					t.Errorf("DefaultCAPackagePolicy: got %q, want %q", s.DefaultCAPackagePolicy, v1alpha1.DefaultCAPackagePolicyEnabled)
				}
				if s.FilterExpiredCertificatesPolicy != v1alpha1.FilterExpiredCertificatesPolicyEnabled {
					t.Errorf("FilterExpiredCertificatesPolicy: got %q, want %q", s.FilterExpiredCertificatesPolicy, v1alpha1.FilterExpiredCertificatesPolicyEnabled)
				}
			},
		},
		{
			name: "default trust namespace is reflected when spec trustNamespace is empty",
			trustManager: func() *v1alpha1.TrustManager {
				tm := testTrustManager().Build()
				tm.Spec.TrustManagerConfig.TrustNamespace = ""
				return tm
			},
			wantStatusUpdate: 1,
			assertStatus: func(t *testing.T, tm *v1alpha1.TrustManager) {
				if tm.Status.TrustNamespace != defaultTrustNamespace {
					t.Errorf("TrustNamespace: got %q, want %q", tm.Status.TrustNamespace, defaultTrustNamespace)
				}
			},
		},
		{
			name: "no-op when observed state already matches spec and env",
			trustManager: func() *v1alpha1.TrustManager {
				tm := testTrustManager().Build()
				tm.Status.TrustManagerImage = testImage
				tm.Status.TrustNamespace = defaultTrustNamespace
				tm.Status.SecretTargetsPolicy = tm.Spec.TrustManagerConfig.SecretTargets.Policy
				tm.Status.DefaultCAPackagePolicy = tm.Spec.TrustManagerConfig.DefaultCAPackage.Policy
				tm.Status.FilterExpiredCertificatesPolicy = tm.Spec.TrustManagerConfig.FilterExpiredCertificates
				return tm
			},
			wantStatusUpdate: 0,
			assertStatus:     func(*testing.T, *v1alpha1.TrustManager) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			tm := tt.trustManager()

			mock.GetCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
				switch o := obj.(type) {
				case *v1alpha1.TrustManager:
					tm.DeepCopyInto(o)
				}
				return nil
			})
			mock.StatusUpdateCalls(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
				return nil
			})
			r.CtrlClient = mock

			if err := r.updateStatusObservedState(tm); err != nil {
				t.Fatalf("updateStatusObservedState: %v", err)
			}
			if got := mock.StatusUpdateCallCount(); got != tt.wantStatusUpdate {
				t.Errorf("StatusUpdateCallCount() = %d, want %d", got, tt.wantStatusUpdate)
			}
			tt.assertStatus(t, tm)
		})
	}
}
