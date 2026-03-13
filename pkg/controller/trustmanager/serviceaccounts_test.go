package trustmanager

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestServiceAccountObject(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		wantName        string
		wantNamespace   string
		wantLabels      map[string]string
		wantAnnotations map[string]string
	}{
		{
			name:          "sets correct name and namespace",
			tm:            testTrustManager(),
			wantName:      trustManagerServiceAccountName,
			wantNamespace: operandNamespace,
		},
		{
			name: "default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			tm := tt.tm.Build()
			sa := r.getServiceAccountObject(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && sa.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, sa.Name)
			}
			if tt.wantNamespace != "" && sa.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, sa.Namespace)
			}
			for key, val := range tt.wantLabels {
				if sa.Labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, sa.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if sa.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, sa.Annotations[key])
				}
			}
		})
	}
}

func TestServiceAccountReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name: "successful apply when not found",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "skip apply when existing matches desired",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					sa := r.getServiceAccountObject(testResourceLabels(), testResourceAnnotations())
					sa.DeepCopyInto(obj.(*corev1.ServiceAccount))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "apply when existing has label drift",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					sa := r.getServiceAccountObject(testResourceLabels(), testResourceAnnotations())
					sa.Labels["app.kubernetes.io/instance"] = "modified-value"
					sa.DeepCopyInto(obj.(*corev1.ServiceAccount))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
					sa := r.getServiceAccountObject(getResourceLabels(tm), getResourceAnnotations(tm))
					sa.Annotations["user-annotation"] = "tampered"
					sa.DeepCopyInto(obj.(*corev1.ServiceAccount))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "exists error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if serviceaccount",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "patch error propagates",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, key client.ObjectKey, obj client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply serviceaccount",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.CtrlClient = mock

			tmBuilder := tt.tmBuilder
			if tmBuilder == nil {
				tmBuilder = testTrustManager()
			}
			tm := tmBuilder.Build()
			err := r.createOrApplyServiceAccounts(tm, getResourceLabels(tm), getResourceAnnotations(tm))
			assertError(t, err, tt.wantErr)

			if got := mock.ExistsCallCount(); got != tt.wantExistsCount {
				t.Errorf("expected %d Exists calls, got %d", tt.wantExistsCount, got)
			}
			if got := mock.PatchCallCount(); got != tt.wantPatchCount {
				t.Errorf("expected %d Patch calls, got %d", tt.wantPatchCount, got)
			}
		})
	}
}
