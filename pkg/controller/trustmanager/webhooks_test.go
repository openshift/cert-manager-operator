package trustmanager

import (
	"context"
	"fmt"
	"testing"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestValidatingWebhookConfigObject(t *testing.T) {
	expectedCAAnnotation := fmt.Sprintf("%s/%s", operandNamespace, trustManagerCertificateName)

	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		wantName        string
		wantLabels      map[string]string
		wantAnnotations map[string]string
		wantServiceName string
		wantServiceNS   string
	}{
		{
			name:     "sets correct name and labels",
			tm:       testTrustManager(),
			wantName: trustManagerWebhookConfigName,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "sets CA injection annotation",
			tm:   testTrustManager(),
			wantAnnotations: map[string]string{
				"cert-manager.io/inject-ca-from": expectedCAAnnotation,
			},
		},
		{
			name: "CA injection annotation not overrideable by user",
			tm: testTrustManager().WithAnnotations(map[string]string{
				"cert-manager.io/inject-ca-from": "should-be-overridden",
				"user-annotation":                "preserved",
			}),
			wantAnnotations: map[string]string{
				"cert-manager.io/inject-ca-from": expectedCAAnnotation,
				"user-annotation":                "preserved",
			},
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
		{
			name:            "configures correct service reference",
			tm:              testTrustManager(),
			wantServiceName: trustManagerServiceName,
			wantServiceNS:   operandNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			vwc := getValidatingWebhookConfigObject(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && vwc.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, vwc.Name)
			}
			for key, val := range tt.wantLabels {
				if vwc.Labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, vwc.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if vwc.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, vwc.Annotations[key])
				}
			}
			if tt.wantServiceName != "" {
				for i, wh := range vwc.Webhooks {
					if wh.ClientConfig.Service == nil {
						t.Errorf("webhook[%d]: expected service reference", i)
						continue
					}
					if wh.ClientConfig.Service.Name != tt.wantServiceName {
						t.Errorf("webhook[%d]: expected service name %q, got %q", i, tt.wantServiceName, wh.ClientConfig.Service.Name)
					}
					if wh.ClientConfig.Service.Namespace != tt.wantServiceNS {
						t.Errorf("webhook[%d]: expected service namespace %q, got %q", i, tt.wantServiceNS, wh.ClientConfig.Service.Namespace)
					}
				}
			}
		})
	}
}

func TestValidatingWebhookConfigReconciliation(t *testing.T) {
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "skip apply when existing matches desired",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					vwc := getValidatingWebhookConfigObject(testResourceLabels(), testResourceAnnotations())
					vwc.DeepCopyInto(obj.(*admissionregistrationv1.ValidatingWebhookConfiguration))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "apply when existing has label drift",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					vwc := getValidatingWebhookConfigObject(testResourceLabels(), testResourceAnnotations())
					vwc.Labels["app.kubernetes.io/instance"] = "modified-value"
					vwc.DeepCopyInto(obj.(*admissionregistrationv1.ValidatingWebhookConfiguration))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
				labels := getResourceLabels(tm)
				annotations := getResourceAnnotations(tm)
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					vwc := getValidatingWebhookConfigObject(labels, annotations)
					vwc.Annotations["user-annotation"] = "tampered"
					vwc.DeepCopyInto(obj.(*admissionregistrationv1.ValidatingWebhookConfiguration))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has service reference drift",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					vwc := getValidatingWebhookConfigObject(testResourceLabels(), testResourceAnnotations())
					vwc.Webhooks[0].ClientConfig.Service.Name = "wrong-service"
					vwc.DeepCopyInto(obj.(*admissionregistrationv1.ValidatingWebhookConfiguration))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has failure policy drift",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					vwc := getValidatingWebhookConfigObject(testResourceLabels(), testResourceAnnotations())
					vwc.Webhooks[0].FailurePolicy = ptr.To(admissionregistrationv1.Ignore)
					vwc.DeepCopyInto(obj.(*admissionregistrationv1.ValidatingWebhookConfiguration))
					return true, nil
				})
			},
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "exists error propagates",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if validatingwebhookconfiguration",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "patch error propagates",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply validatingwebhookconfiguration",
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
			err := r.createOrApplyValidatingWebhookConfiguration(tm, getResourceLabels(tm), getResourceAnnotations(tm))
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
