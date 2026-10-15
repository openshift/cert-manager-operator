package trustmanager

import (
	"context"
	"reflect"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestUpdateStatusObservedState(t *testing.T) {
	t.Setenv(trustManagerImageNameEnvVarName, testImage)

	wantImageStatus := v1alpha1.TrustManagerStatus{
		TrustManagerImage: testImage,
	}

	tests := []struct {
		name             string
		trustManager     func() *v1alpha1.TrustManager
		wantStatusUpdate int
		wantStatus       v1alpha1.TrustManagerStatus
	}{
		{
			name: "sets trust-manager image when status is empty",
			trustManager: func() *v1alpha1.TrustManager {
				return testTrustManager().Build()
			},
			wantStatusUpdate: 1,
			wantStatus:       wantImageStatus,
		},
		{
			name: "does not echo spec fields into status",
			trustManager: func() *v1alpha1.TrustManager {
				return testTrustManager().
					WithTrustNamespace("custom-trust-ns").
					WithSecretTargets(v1alpha1.SecretTargetsPolicyCustom, []string{"allowed-secret"}).
					WithDefaultCAPackage(v1alpha1.DefaultCAPackagePolicy(v1alpha1.Enabled)).
					WithFilterExpiredCertificates(v1alpha1.FilterExpiredCertificatesPolicy(v1alpha1.Enabled)).
					WithFilterNonCACerts(v1alpha1.FilterNonCACertsPolicy(v1alpha1.Enabled)).
					Build()
			},
			wantStatusUpdate: 1,
			wantStatus:       wantImageStatus,
		},
		{
			name: "no-op when image already matches env",
			trustManager: func() *v1alpha1.TrustManager {
				tm := testTrustManager().Build()
				tm.Status.TrustManagerImage = testImage
				return tm
			},
			wantStatusUpdate: 0,
			wantStatus:       wantImageStatus,
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

			if !reflect.DeepEqual(tm.Status, tt.wantStatus) {
				t.Errorf("TrustManager.Status mismatch (-want +got):\n%s", cmp.Diff(tt.wantStatus, tm.Status))
			}
		})
	}
}
