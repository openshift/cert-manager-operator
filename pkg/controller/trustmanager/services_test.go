package trustmanager

import (
	"context"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestServiceObject(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		getService      func(map[string]string, map[string]string) *corev1.Service
		wantName        string
		wantNamespace   string
		wantLabels      map[string]string
		wantAnnotations map[string]string
	}{
		{
			name:          "webhook service sets correct name and namespace",
			tm:            testTrustManager(),
			getService:    getWebhookServiceObject,
			wantName:      trustManagerServiceName,
			wantNamespace: operandNamespace,
		},
		{
			name:          "metrics service sets correct name and namespace",
			tm:            testTrustManager(),
			getService:    getMetricsServiceObject,
			wantName:      trustManagerMetricsServiceName,
			wantNamespace: operandNamespace,
		},
		{
			name:       "default labels take precedence over user labels",
			tm:         testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			getService: getWebhookServiceObject,
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
		{
			name: "merges custom labels and annotations",
			tm: testTrustManager().
				WithLabels(map[string]string{"user-label": "test-value"}).
				WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			getService:      getWebhookServiceObject,
			wantLabels:      map[string]string{"user-label": "test-value"},
			wantAnnotations: map[string]string{"user-annotation": "test-value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := tt.tm.Build()
			svc := tt.getService(getResourceLabels(tm), getResourceAnnotations(tm))

			if tt.wantName != "" && svc.Name != tt.wantName {
				t.Errorf("expected name %q, got %q", tt.wantName, svc.Name)
			}
			if tt.wantNamespace != "" && svc.Namespace != tt.wantNamespace {
				t.Errorf("expected namespace %q, got %q", tt.wantNamespace, svc.Namespace)
			}
			for key, val := range tt.wantLabels {
				if svc.Labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, svc.Labels[key])
				}
			}
			for key, val := range tt.wantAnnotations {
				if svc.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, svc.Annotations[key])
				}
			}
		})
	}
}

func TestServiceReconciliation(t *testing.T) {
	tests := []struct {
		name            string
		tmBuilder       *trustManagerBuilder
		preReq          func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr         string
		wantExistsCount int
		wantPatchCount  int
	}{
		{
			name: "successful apply of both services",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, nil
				})
			},
			wantExistsCount: 2,
			wantPatchCount:  2,
		},
		{
			name: "skip apply when both services match desired",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					var svc *corev1.Service
					if existsCall == 1 {
						svc = getWebhookServiceObject(testResourceLabels(), testResourceAnnotations())
					} else {
						svc = getMetricsServiceObject(testResourceLabels(), testResourceAnnotations())
					}
					svc.DeepCopyInto(obj.(*corev1.Service))
					return true, nil
				})
			},
			wantExistsCount: 2,
			wantPatchCount:  0,
		},
		{
			name: "apply when existing has label drift",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					if existsCall == 1 {
						svc := getWebhookServiceObject(testResourceLabels(), testResourceAnnotations())
						svc.Labels["app.kubernetes.io/instance"] = "modified-value"
						svc.DeepCopyInto(obj.(*corev1.Service))
					} else {
						svc := getMetricsServiceObject(testResourceLabels(), testResourceAnnotations())
						svc.DeepCopyInto(obj.(*corev1.Service))
					}
					return true, nil
				})
			},
			wantExistsCount: 2,
			wantPatchCount:  1,
		},
		{
			name:      "apply when existing has annotation drift",
			tmBuilder: testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}),
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				tm := testTrustManager().WithAnnotations(map[string]string{"user-annotation": "original"}).Build()
				labels := getResourceLabels(tm)
				annotations := getResourceAnnotations(tm)
				existsCall := 0
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					if existsCall == 1 {
						svc := getWebhookServiceObject(labels, annotations)
						svc.Annotations["user-annotation"] = "tampered"
						svc.DeepCopyInto(obj.(*corev1.Service))
					} else {
						svc := getMetricsServiceObject(labels, annotations)
						svc.DeepCopyInto(obj.(*corev1.Service))
					}
					return true, nil
				})
			},
			wantExistsCount: 2,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has port drift",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					if existsCall == 1 {
						svc := getWebhookServiceObject(testResourceLabels(), testResourceAnnotations())
						svc.Spec.Ports[0].TargetPort = intstr.FromInt32(9999)
						svc.DeepCopyInto(obj.(*corev1.Service))
					} else {
						svc := getMetricsServiceObject(testResourceLabels(), testResourceAnnotations())
						svc.DeepCopyInto(obj.(*corev1.Service))
					}
					return true, nil
				})
			},
			wantExistsCount: 2,
			wantPatchCount:  1,
		},
		{
			name: "apply when existing has selector drift",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				existsCall := 0
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, obj client.Object) (bool, error) {
					existsCall++
					if existsCall == 1 {
						svc := getWebhookServiceObject(testResourceLabels(), testResourceAnnotations())
						svc.Spec.Selector["app"] = "wrong-selector"
						svc.DeepCopyInto(obj.(*corev1.Service))
					} else {
						svc := getMetricsServiceObject(testResourceLabels(), testResourceAnnotations())
						svc.DeepCopyInto(obj.(*corev1.Service))
					}
					return true, nil
				})
			},
			wantExistsCount: 2,
			wantPatchCount:  1,
		},
		{
			name: "exists error propagates on first service",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, errTestClient
				})
			},
			wantErr:         "failed to check if service",
			wantExistsCount: 1,
			wantPatchCount:  0,
		},
		{
			name: "webhook service patch error propagates",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, nil
				})
				m.PatchCalls(func(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					return errTestClient
				})
			},
			wantErr:         "failed to apply service",
			wantExistsCount: 1,
			wantPatchCount:  1,
		},
		{
			name: "metrics service patch error propagates on second call",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(_ context.Context, _ client.ObjectKey, _ client.Object) (bool, error) {
					return false, nil
				})
				callCount := 0
				m.PatchCalls(func(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
					callCount++
					if callCount == 2 {
						return errTestClient
					}
					return nil
				})
			},
			wantErr:         "failed to apply service",
			wantExistsCount: 2,
			wantPatchCount:  2,
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
			err := r.createOrApplyServices(tm, getResourceLabels(tm), getResourceAnnotations(tm))
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
