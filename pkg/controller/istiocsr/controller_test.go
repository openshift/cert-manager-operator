package istiocsr

import (
	"context"
	"reflect"
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

	t.Run("successful cases", func(t *testing.T) {
		testReconcile_Successful(t)
	})
	t.Run("error cases", func(t *testing.T) {
		testReconcile_Errors(t)
	})
	t.Run("resource state cases", func(t *testing.T) {
		testReconcile_ResourceStates(t)
	})
	t.Run("finalizer cases", func(t *testing.T) {
		testReconcile_Finalizers(t)
	})
}

func testReconcile_Successful(t *testing.T) {
	tests := []struct {
		name                    string
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name: "reconciliation successful",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.ReasonReady,
					Message: "reconciliation successful",
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
			},
			requeue: false,
		},
	}
	runReconcileTests(t, tests)
}

func testReconcile_Errors(t *testing.T) {
	t.Run("recoverable errors", func(t *testing.T) {
		testReconcile_Errors_Recoverable(t)
	})
	t.Run("irrecoverable errors", func(t *testing.T) {
		testReconcile_Errors_Irrecoverable(t)
	})
}

func testReconcile_Errors_Recoverable(t *testing.T) {
	tests := []struct {
		name                    string
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name: "reconciliation failed",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					case *certmanagerv1.Certificate:
						cert := testCertificate()
						cert.DeepCopyInto(o)
					case *rbacv1.ClusterRole:
						role := testClusterRole()
						role.DeepCopyInto(o)
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *rbacv1.Role:
						var role *rbacv1.Role
						if strings.HasSuffix(ns.Name, "-leases") {
							role = testRoleLeases()
						} else {
							role = testRole()
						}
						role.DeepCopyInto(o)
					case *rbacv1.RoleBinding:
						var roleBinding *rbacv1.RoleBinding
						if strings.HasSuffix(ns.Name, "-leases") {
							roleBinding = testRoleBindingLeases()
						} else {
							roleBinding = testRoleBinding()
						}
						roleBinding.DeepCopyInto(o)
					case *corev1.Service:
						service := testService()
						service.DeepCopyInto(o)
					case *corev1.ServiceAccount:
						serviceAccount := testServiceAccount()
						serviceAccount.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						// fail the operation where controller is trying to add processed annotation.
						if _, exist := o.GetAnnotations()[controllerProcessedAnnotation]; exist {
							return errTestClient
						}
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
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
			requeue: false,
			wantErr: `failed to update processed annotation: failed to update processed annotation to istiocsr-test-ns/istiocsr-test-resource: test client error`,
		},
	}
	runReconcileTests(t, tests)
}

func testReconcile_Errors_Irrecoverable(t *testing.T) {
	tests := []struct {
		name                    string
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name: "reconciliation failed with irrecoverable error",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					case *certmanagerv1.Certificate:
						cert := testCertificate()
						cert.DeepCopyInto(o)
					case *rbacv1.ClusterRole:
						role := testClusterRole()
						role.DeepCopyInto(o)
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *rbacv1.Role:
						var role *rbacv1.Role
						if strings.HasSuffix(ns.Name, "-leases") {
							role = testRoleLeases()
						} else {
							role = testRole()
						}
						role.DeepCopyInto(o)
					case *rbacv1.RoleBinding:
						var roleBinding *rbacv1.RoleBinding
						if strings.HasSuffix(ns.Name, "-leases") {
							roleBinding = testRoleBindingLeases()
						} else {
							roleBinding = testRoleBinding()
						}
						roleBinding.DeepCopyInto(o)
					case *corev1.Service:
						service := testService()
						service.DeepCopyInto(o)
					case *corev1.ServiceAccount:
						serviceAccount := testServiceAccount()
						serviceAccount.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *appsv1.Deployment:
						return false, nil
					case *corev1.ConfigMap:
						return true, nil
					}
					return true, nil
				})
				// fail the operation where controller is trying to create deployment resource.
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						return apierrors.NewUnauthorized("test error")
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
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
	}
	runReconcileTests(t, tests)
}

func testReconcile_ResourceStates(t *testing.T) {
	tests := []struct {
		name                    string
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name: "reconciliation istiocsr resource does not exist",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
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
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateReturns(nil)
				m.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
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
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource status: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test error`,
		},
	}
	runReconcileTests(t, tests)
}

func testReconcile_Finalizers(t *testing.T) {
	tests := []struct {
		name                    string
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name: "reconciliation remove finalizer from istiocsr fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeletionTimestamp = &metav1.Time{Time: time.Now()}
						istiocsr.Finalizers = []string{finalizer}
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return errTestClient
					}
					return nil
				})
			},
			requeue: false,
			wantErr: `failed to remove finalizers on "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with test client error`,
		},
		{
			name: "reconciliation adding finalizer to istiocsr fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return errTestClient
					}
					return nil
				})
			},
			requeue: false,
			wantErr: `failed to update "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with finalizers: failed to add finalizers on "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with test client error`,
		},
	}
	runReconcileTests(t, tests)
}

// runReconcileTests is a helper function to run reconcile test cases and reduce cyclomatic complexity.
func runReconcileTests(t *testing.T, tests []struct {
	name                    string
	preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
	expectedStatusCondition []metav1.Condition
	requeue                 bool
	wantErr                 string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
			istiocsr := testIstioCSR()
			result, err := r.Reconcile(context.Background(),
				ctrl.Request{
					NamespacedName: types.NamespacedName{Name: istiocsr.GetName(), Namespace: istiocsr.GetNamespace()},
				},
			)
			validateReconcileResult(t, err, tt.wantErr, tt.requeue, result, istiocsr, tt.expectedStatusCondition)
		})
	}
}

// validateReconcileResult validates the result of a reconcile operation.
func validateReconcileResult(t *testing.T, err error, wantErr string, requeue bool, result ctrl.Result, istiocsr *v1alpha1.IstioCSR, expectedStatusCondition []metav1.Condition) {
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("Reconcile() err: %v, wantErr: %v", err, wantErr)
	}
	if requeue && result.IsZero() {
		t.Errorf("Reconcile() expected requeue to be set")
	}
	validateStatusConditions(t, istiocsr.Status.Conditions, expectedStatusCondition)
}

// validateStatusConditions validates that the actual conditions match the expected conditions.
func validateStatusConditions(t *testing.T, actual []metav1.Condition, expected []metav1.Condition) {
	for _, c1 := range actual {
		for _, c2 := range expected {
			if c1.Type == c2.Type {
				if c1.Status != c2.Status || c1.Reason != c2.Reason {
					t.Errorf("Reconcile() condition: %+v, expectedStatusCondition: %+v", c1, c2)
				}
			}
		}
	}
}

func TestProcessReconcileRequest(t *testing.T) {
	// set the operand image env var
	t.Setenv("RELATED_IMAGE_CERT_MANAGER_ISTIOCSR", "registry.redhat.io/cert-manager/cert-manager-istio-csr-rhel9:latest")

	t.Run("successful cases", func(t *testing.T) {
		testProcessReconcileRequest_Successful(t)
	})
	t.Run("duplicate cases", func(t *testing.T) {
		testProcessReconcileRequest_Duplicates(t)
	})
}

func testProcessReconcileRequest_Successful(t *testing.T) {
	tests := []struct {
		name                    string
		getIstioCSR             func() *v1alpha1.IstioCSR
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		expectedAnnotations     map[string]string
		wantErr                 string
	}{
		{
			name: "reconciliation successful",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				return testIstioCSR()
			},
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.ReasonReady,
					Message: "reconciliation successful",
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
			},
			expectedAnnotations: map[string]string{
				controllerProcessedAnnotation: "true",
			},
		},
	}
	runProcessReconcileRequestTests(t, tests)
}

func testProcessReconcileRequest_Duplicates(t *testing.T) {
	tests := []struct {
		name                    string
		getIstioCSR             func() *v1alpha1.IstioCSR
		preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		expectedAnnotations     map[string]string
		wantErr                 string
	}{
		{
			name: "reconciliation of existing istiocsr",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				istiocsr := testIstioCSR()
				istiocsr.Annotations = map[string]string{
					controllerProcessedAnnotation: "true",
				}
				return istiocsr
			},
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch o := obj.(type) {
					case *rbacv1.ClusterRoleBinding:
						roleBinding := testClusterRoleBinding()
						roleBinding.DeepCopyInto(o)
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionTrue,
					Reason:  v1alpha1.ReasonReady,
					Message: "reconciliation successful",
				},
				{
					Type:   v1alpha1.Degraded,
					Status: metav1.ConditionFalse,
					Reason: v1alpha1.ReasonReady,
				},
			},
			expectedAnnotations: map[string]string{
				controllerProcessedAnnotation: "true",
			},
		},
		{
			name: "reconciliation of duplicate istiocsr with annotation present",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				istiocsr := testIstioCSR()
				istiocsr.Annotations = map[string]string{
					controllerProcessingRejectedAnnotation: "true",
				}
				return istiocsr
			},
			preReq: func(_ *Reconciler, _ *fakes.FakeCtrlClient) {},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonFailed,
					Message: "multiple instances of istiocsr exists, istiocsr-test-ns/istiocsr-test-resource will not be processed",
				},
			},
			expectedAnnotations: map[string]string{
				controllerProcessingRejectedAnnotation: "true",
			},
		},
		{
			name: "validating multiple istiocsrs' fails while listing",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				return testIstioCSR()
			},
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, list client.ObjectList, option ...client.ListOption) error {
					switch list.(type) {
					case *v1alpha1.IstioCSRList:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to fetch list of istiocsr resources: test client error`,
		},
		{
			name: "validating multiple istiocsrs' successful by ignoring latest instance",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				istiocsr := testIstioCSR()
				istiocsr.SetNamespace("istiocsr3")
				istiocsr.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))
				return istiocsr
			},
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, list client.ObjectList, option ...client.ListOption) error {
					switch l := list.(type) {
					case *v1alpha1.IstioCSRList:
						istiocsr1 := testIstioCSR()
						istiocsr2 := testIstioCSR()
						istiocsr3 := testIstioCSR()

						istiocsr1.SetNamespace("istiocsr1")
						istiocsr2.SetNamespace("istiocsr2")
						istiocsr3.SetNamespace("istiocsr3")

						istiocsr1.SetCreationTimestamp(metav1.NewTime(time.Now()))
						istiocsr2.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second)))
						istiocsr3.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))

						l.Items = []v1alpha1.IstioCSR{*istiocsr1, *istiocsr2, *istiocsr3}
					}
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonFailed,
					Message: "multiple instances of istiocsr exists, istiocsr3/istiocsr-test-resource will not be processed",
				},
			},
			expectedAnnotations: map[string]string{
				controllerProcessingRejectedAnnotation: "true",
			},
		},
		{
			name: "validating multiple istiocsrs' failed to update status",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				istiocsr := testIstioCSR()
				istiocsr.SetNamespace("istiocsr3")
				istiocsr.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))
				return istiocsr
			},
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, list client.ObjectList, option ...client.ListOption) error {
					switch l := list.(type) {
					case *v1alpha1.IstioCSRList:
						istiocsr1 := testIstioCSR()
						istiocsr2 := testIstioCSR()
						istiocsr3 := testIstioCSR()

						istiocsr1.SetNamespace("istiocsr1")
						istiocsr2.SetNamespace("istiocsr2")
						istiocsr3.SetNamespace("istiocsr3")

						istiocsr1.SetCreationTimestamp(metav1.NewTime(time.Now()))
						istiocsr2.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second)))
						istiocsr3.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))

						l.Items = []v1alpha1.IstioCSR{*istiocsr1, *istiocsr2, *istiocsr3}
					}
					m.StatusUpdateCalls(func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
						switch obj.(type) {
						case *v1alpha1.IstioCSR:
							return errTestClient
						}
						return nil
					})
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonFailed,
					Message: "multiple instances of istiocsr exists, istiocsr3/istiocsr-test-resource will not be processed",
				},
			},
			expectedAnnotations: map[string]string{
				controllerProcessingRejectedAnnotation: "true",
			},
			wantErr: `failed to handle ignore processing: failed to update istiocsr3/istiocsr-test-resource status: failed to update status for "istiocsr3/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr3/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "validating multiple istiocsrs' failed to update annotations",
			getIstioCSR: func() *v1alpha1.IstioCSR {
				istiocsr := testIstioCSR()
				istiocsr.SetNamespace("istiocsr3")
				istiocsr.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))
				return istiocsr
			},
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.ListCalls(func(ctx context.Context, list client.ObjectList, option ...client.ListOption) error {
					switch l := list.(type) {
					case *v1alpha1.IstioCSRList:
						istiocsr1 := testIstioCSR()
						istiocsr2 := testIstioCSR()
						istiocsr3 := testIstioCSR()

						istiocsr1.SetNamespace("istiocsr1")
						istiocsr2.SetNamespace("istiocsr2")
						istiocsr3.SetNamespace("istiocsr3")

						istiocsr1.SetCreationTimestamp(metav1.NewTime(time.Now()))
						istiocsr2.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second)))
						istiocsr3.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))

						l.Items = []v1alpha1.IstioCSR{*istiocsr1, *istiocsr2, *istiocsr3}
					}
					m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, option ...client.UpdateOption) error {
						switch obj.(type) {
						case *v1alpha1.IstioCSR:
							return errTestClient
						}
						return nil
					})
					return nil
				})
			},
			expectedStatusCondition: []metav1.Condition{
				{
					Type:    v1alpha1.Ready,
					Status:  metav1.ConditionFalse,
					Reason:  v1alpha1.ReasonFailed,
					Message: "multiple instances of istiocsr exists, istiocsr3/istiocsr-test-resource will not be processed",
				},
			},
			expectedAnnotations: map[string]string{
				controllerProcessingRejectedAnnotation: "true",
			},
			wantErr: `failed to handle ignore processing: failed to update reject processing annotation to istiocsr3/istiocsr-test-resource: test client error`,
		},
	}
	runProcessReconcileRequestTests(t, tests)
}

// runProcessReconcileRequestTests is a helper function to run processReconcileRequest test cases and reduce cyclomatic complexity.
func runProcessReconcileRequestTests(t *testing.T, tests []struct {
	name                    string
	getIstioCSR             func() *v1alpha1.IstioCSR
	preReq                  func(*Reconciler, *fakes.FakeCtrlClient)
	expectedStatusCondition []metav1.Condition
	expectedAnnotations     map[string]string
	wantErr                 string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
			istiocsr := tt.getIstioCSR()
			_, err := r.processReconcileRequest(istiocsr,
				types.NamespacedName{Name: istiocsr.GetName(), Namespace: istiocsr.GetNamespace()})
			validateProcessReconcileRequestResult(t, err, tt.wantErr, istiocsr, tt.expectedAnnotations, tt.expectedStatusCondition)
		})
	}
}

// validateProcessReconcileRequestResult validates the result of a processReconcileRequest operation.
func validateProcessReconcileRequestResult(t *testing.T, err error, wantErr string, istiocsr *v1alpha1.IstioCSR, expectedAnnotations map[string]string, expectedStatusCondition []metav1.Condition) {
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("processReconcileRequest() err: %v, wantErr: %v", err, wantErr)
	}
	if !reflect.DeepEqual(istiocsr.Annotations, expectedAnnotations) {
		t.Errorf("processReconcileRequest() annotations: %v, expectedAnnotations: %v", istiocsr.Annotations, expectedAnnotations)
	}
	validateProcessReconcileRequestConditions(t, istiocsr.Status.Conditions, expectedStatusCondition)
}

// validateProcessReconcileRequestConditions validates that the actual conditions match the expected conditions.
func validateProcessReconcileRequestConditions(t *testing.T, actual []metav1.Condition, expected []metav1.Condition) {
	for _, c1 := range actual {
		for _, c2 := range expected {
			if c1.Type == c2.Type {
				if c1.Status != c2.Status || c1.Reason != c2.Reason || c1.Message != c2.Message {
					t.Errorf("processReconcileRequest() condition: %+v, expectedStatusCondition: %+v", c1, c2)
				}
			}
		}
	}
}
