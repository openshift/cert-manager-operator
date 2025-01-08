package istiocsr

import (
	"context"
	"strings"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

func TestReconcile(t *testing.T) {
	// set the operand image env var
	t.Setenv("RELATED_IMAGE_CERT_MANAGER_ISTIOCSR", "registry.redhat.io/cert-manager/cert-manager-istio-csr-rhel9:latest")

	tests := []struct {
		name                    string
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name: "reconciliation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding(t)
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.ReasonReady,
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
			},
			requeue: false,
		},
		{
			name: "reconciliation failed",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeepCopyInto(o)
					case *certmanagerv1.Certificate:
						cert := testCertificate(t)
						cert.DeepCopyInto(o)
					case *rbacv1.ClusterRole:
						role := testClusterRole(t)
						role.DeepCopyInto(o)
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding(t)
						roleBinding.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *rbacv1.Role:
						var role *rbacv1.Role
						if strings.HasSuffix(ns.Name, "-leases") {
							role = testRoleLeases(t)
						} else {
							role = testRole(t)
						}
						role.DeepCopyInto(o)
					case *rbacv1.RoleBinding:
						var roleBinding *rbacv1.RoleBinding
						if strings.HasSuffix(ns.Name, "-leases") {
							roleBinding = testRoleBindingLeases(t)
						} else {
							roleBinding = testRoleBinding(t)
						}
						roleBinding.DeepCopyInto(o)
					case *corev1.Service:
						service := testService(t)
						service.DeepCopyInto(o)
					case *corev1.ServiceAccount:
						serviceAccount := testServiceAccount(t)
						serviceAccount.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						// fail the operation where controller is trying to add processed annotation.
						if _, exist := o.GetAnnotations()[controllerProcessedAnnotation]; exist {
							return testError
						}
					}
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding(t)
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonFailed,
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
			},
			requeue: true,
			wantErr: `failed to reconcile "istiocsr-test-ns/istiocsr-test-resource" IstioCSR deployment: failed to update processed annotation to istiocsr-test-ns/istiocsr-test-resource: test client error`,
		},
		{
			name: "reconciliation failed with irrecoverable error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeepCopyInto(o)
					case *certmanagerv1.Certificate:
						cert := testCertificate(t)
						cert.DeepCopyInto(o)
					case *rbacv1.ClusterRole:
						role := testClusterRole(t)
						role.DeepCopyInto(o)
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding(t)
						roleBinding.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *rbacv1.Role:
						var role *rbacv1.Role
						if strings.HasSuffix(ns.Name, "-leases") {
							role = testRoleLeases(t)
						} else {
							role = testRole(t)
						}
						role.DeepCopyInto(o)
					case *rbacv1.RoleBinding:
						var roleBinding *rbacv1.RoleBinding
						if strings.HasSuffix(ns.Name, "-leases") {
							roleBinding = testRoleBindingLeases(t)
						} else {
							roleBinding = testRoleBinding(t)
						}
						roleBinding.DeepCopyInto(o)
					case *corev1.Service:
						service := testService(t)
						service.DeepCopyInto(o)
					case *corev1.ServiceAccount:
						serviceAccount := testServiceAccount(t)
						serviceAccount.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *appsv1.Deployment:
						return false, nil
					}
					return true, nil
				})
				// fail the operation where controller is trying to create deployment resource.
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						return apierrors.NewUnauthorized("test error")
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding(t)
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonFailed,
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.ReasonFailed,
				},
			},
			requeue: false,
			// reconcile does not report back irrecoverable errors
			wantErr: ``,
		},
		{
			name: "reconciliation istiocsr resource does not exist",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return apierrors.NewNotFound(v1alpha1.Resource("istiocsr"), "default")
					}
					return nil
				})
			},
			requeue: false,
		},
		{
			name: "reconciliation failed to fetch istiocsr resource",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return apierrors.NewBadRequest("test error")
					}
					return nil
				})
			},
			requeue: false,
			wantErr: `failed to fetch istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" during reconciliation: test error`,
		},
		{
			name: "reconciliation istiocsr marked for deletion",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeletionTimestamp = &metav1.Time{Time: time.Now()}
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
			requeue: false,
		},
		{
			name: "reconciliation updating istiocsr status subresource failed",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateReturns(nil)
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return apierrors.NewBadRequest("test error")
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:   v1alpha1.Ready,
					Status: metav1.ConditionTrue,
					Reason: v1alpha1.ReasonReady,
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
			},
			requeue: false,
			wantErr: `failed to update "istiocsr-test-ns/istiocsr-test-resource" status: failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test error`,
		},
		{
			name: "reconciliation remove finalizer from istiocsr fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeletionTimestamp = &metav1.Time{Time: time.Now()}
						istiocsr.Finalizers = []string{finalizer}
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return testError
					}
					return nil
				})
			},
			requeue: false,
			wantErr: `failed to remove finalizers on "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with test client error`,
		},
		{
			name: "reconciliation adding finalizer to istiocsr fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR(t)
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return testError
					}
					return nil
				})
			},
			requeue: false,
			wantErr: `failed to update "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with finalizers: failed to add finalizers on "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with test client error`,
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
			istiocsr := testIstioCSR(t)
			result, err := r.Reconcile(context.Background(),
				ctrl.Request{
					NamespacedName: types.NamespacedName{Name: istiocsr.GetName(), Namespace: istiocsr.GetNamespace()},
				},
			)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("Reconcile() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.requeue && result.IsZero() {
				t.Errorf("Reconcile() expected requeue to be set")
			}
			for _, c1 := range istiocsr.Status.Conditions {
				for _, c2 := range tt.expectedStatusCondition {
					if c1.Type == c2.Type {
						if c1.Status != c2.Status || c1.Reason != c2.Reason {
							t.Errorf("Reconcile() condition: %+v, expectedStatusCondition: %+v", c2, c1)
						}
					}
				}
			}
		})
	}
}
