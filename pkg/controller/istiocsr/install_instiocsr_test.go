package istiocsr

import (
	"context"
	"fmt"
	"maps"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

func TestReconcileIstioCSRDeployment(t *testing.T) {
	// set the operand image env var
	t.Setenv("RELATED_IMAGE_CERT_MANAGER_ISTIOCSR", "registry.redhat.io/cert-manager/cert-manager-istio-csr-rhel9:latest")

	istiocsr := testIstioCSR()
	labels := make(map[string]string)
	maps.Copy(labels, controllerDefaultResourceLabels)
	// add user labels
	maps.Copy(labels, istiocsr.Spec.ControllerConfig.Labels)

	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "istiocsr reconciliation with user labels successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
					}
					return nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
					switch o := obj.(type) {
					case *appsv1.Deployment, *corev1.Service, *corev1.ServiceAccount:
						if !reflect.DeepEqual(o.GetLabels(), labels) {
							return fmt.Errorf("labels mismatch in %v resource; got: %v, want: %v", o, o.GetLabels(), labels)
						}
					case *certmanagerv1.Certificate, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding:
						l := make(map[string]string)
						maps.Copy(l, labels)
						l[istiocsrNamespaceMappingLabelName] = testIstioCSRNamespace
						if !reflect.DeepEqual(o.GetLabels(), l) {
							return fmt.Errorf("labels mismatch in %v resource; got: %v, want: %v", o, o.GetLabels(), l)
						}
					}
					return nil
				})
			},
		},
		{
			name: "istiocsr reconciliation fails with serviceaccount creation error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
					switch obj.(type) {
					case *corev1.ServiceAccount:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to reconcile all resources: failed to reconcile serviceaccount resource: failed to create istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource: test client error`,
		},
		{
			name: "istiocsr reconciliation fails with role creation error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.Role:
						return errTestClient
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to reconcile all resources: failed to reconcile rbac resource: failed to create or apply roles: failed to create istio-test-ns/cert-manager-istio-csr role resource: test client error`,
		},
		{
			name: "istiocsr reconciliation fails with certificate creation error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
					switch o := obj.(type) {
					case *certmanagerv1.Certificate:
						return errTestClient
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to reconcile all resources: failed to reconcile certificate resource: failed to create istio-test-ns/istiod certificate resource: test client error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
			err := r.reconcileIstioCSRDeployment(istiocsr, true)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("reconcileIstioCSRDeployment() err: %v, wantErr: %v", err, tt.wantErr)
			}
		})
	}
}
