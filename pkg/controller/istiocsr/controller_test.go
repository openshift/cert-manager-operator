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

// Helper functions to reduce cyclomatic complexity in tests

func setupBasicGetCalls(m *fakes.FakeCtrlClient, includeAllResources bool) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
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
		if includeAllResources {
			switch o := obj.(type) {
			case *certmanagerv1.Certificate:
				cert := testCertificate()
				cert.DeepCopyInto(o)
			case *rbacv1.ClusterRole:
				role := testClusterRole()
				role.DeepCopyInto(o)
			case *rbacv1.ClusterRoleBinding:
				roleBinding := testClusterRoleBinding()
				roleBinding.DeepCopyInto(o)
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
			}
		}
		return nil
	})
}

func setupBasicExistsCalls(m *fakes.FakeCtrlClient) {
	m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
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
}

func setupBasicCreateCalls(m *fakes.FakeCtrlClient) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
		switch o := obj.(type) {
		case *rbacv1.ClusterRoleBinding:
			roleBinding := testClusterRoleBinding()
			roleBinding.DeepCopyInto(o)
		}
		return nil
	})
}

func setupSuccessfulReconciliation(m *fakes.FakeCtrlClient) {
	setupBasicGetCalls(m, false)
	setupBasicExistsCalls(m)
	setupBasicCreateCalls(m)
}

func setupFailedReconciliationWithAnnotationError(m *fakes.FakeCtrlClient) {
	setupBasicGetCalls(m, true)
	setupBasicExistsCalls(m)
	setupBasicCreateCalls(m)
	m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		switch o := obj.(type) {
		case *v1alpha1.IstioCSR:
			if _, exist := o.GetAnnotations()[controllerProcessedAnnotation]; exist {
				return testError
			}
		}
		return nil
	})
}

func setupFailedReconciliationWithDeploymentError(m *fakes.FakeCtrlClient) {
	setupBasicGetCalls(m, true)
	m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
		switch obj.(type) {
		case *appsv1.Deployment:
			return false, nil
		case *corev1.ConfigMap:
			return true, nil
		}
		return true, nil
	})
	m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			return apierrors.NewUnauthorized("test error")
		case *rbacv1.ClusterRoleBinding:
			roleBinding := testClusterRoleBinding()
			roleBinding.DeepCopyInto(o)
		}
		return nil
	})
}

func setupIstioCSRNotFound(m *fakes.FakeCtrlClient) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
		switch obj.(type) {
		case *v1alpha1.IstioCSR:
			return apierrors.NewNotFound(v1alpha1.Resource("istiocsr"), "default")
		}
		return nil
	})
}

func setupIstioCSRFetchError(m *fakes.FakeCtrlClient) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
		switch obj.(type) {
		case *v1alpha1.IstioCSR:
			return apierrors.NewBadRequest("test error")
		}
		return nil
	})
}

func setupIstioCSRMarkedForDeletion(m *fakes.FakeCtrlClient) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
		switch o := obj.(type) {
		case *v1alpha1.IstioCSR:
			istiocsr := testIstioCSR()
			istiocsr.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			istiocsr.DeepCopyInto(o)
		}
		return nil
	})
}

func setupStatusUpdateError(m *fakes.FakeCtrlClient) {
	setupBasicGetCalls(m, false)
	m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
		switch o := obj.(type) {
		case *appsv1.Deployment:
			deployment := testDeployment()
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
}

func setupFinalizerRemovalError(m *fakes.FakeCtrlClient) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
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
			return testError
		}
		return nil
	})
}

func setupFinalizerAdditionError(m *fakes.FakeCtrlClient) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
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
			return testError
		}
		return nil
	})
}

func runReconcileTest(t *testing.T, r *Reconciler, mock *fakes.FakeCtrlClient, istiocsr *v1alpha1.IstioCSR, expectedStatusCondition []metav1.Condition, requeue bool, wantErr string) {
	result, err := r.Reconcile(context.Background(),
		ctrl.Request{
			NamespacedName: types.NamespacedName{Name: istiocsr.GetName(), Namespace: istiocsr.GetNamespace()},
		},
	)
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("Reconcile() err: %v, wantErr: %v", err, wantErr)
	}
	if requeue && result.IsZero() {
		t.Errorf("Reconcile() expected requeue to be set")
	}
	for _, c1 := range istiocsr.Status.Conditions {
		for _, c2 := range expectedStatusCondition {
			if c1.Type == c2.Type {
				if c1.Status != c2.Status || c1.Reason != c2.Reason {
					t.Errorf("Reconcile() condition: %+v, expectedStatusCondition: %+v", c1, c2)
				}
			}
		}
	}
}

func TestReconcile(t *testing.T) {
	// set the operand image env var
	t.Setenv("RELATED_IMAGE_CERT_MANAGER_ISTIOCSR", "registry.redhat.io/cert-manager/cert-manager-istio-csr-rhel9:latest")

	tests := []struct {
		name                    string
		setupMock               func(*fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		requeue                 bool
		wantErr                 string
	}{
		{
			name:      "reconciliation successful",
			setupMock: setupSuccessfulReconciliation,
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
		{
			name:      "reconciliation failed",
			setupMock: setupFailedReconciliationWithAnnotationError,
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
			wantErr: `failed to update processed annotation to istiocsr-test-ns/istiocsr-test-resource: test client error`,
		},
		{
			name:      "reconciliation failed with irrecoverable error",
			setupMock: setupFailedReconciliationWithDeploymentError,
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
			wantErr: ``,
		},
		{
			name:      "reconciliation istiocsr resource does not exist",
			setupMock: setupIstioCSRNotFound,
			requeue:   false,
		},
		{
			name:      "reconciliation failed to fetch istiocsr resource",
			setupMock: setupIstioCSRFetchError,
			requeue:   false,
			wantErr:   `failed to fetch istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" during reconciliation: test error`,
		},
		{
			name:      "reconciliation istiocsr marked for deletion",
			setupMock: setupIstioCSRMarkedForDeletion,
			requeue:   false,
		},
		{
			name:      "reconciliation updating istiocsr status subresource failed",
			setupMock: setupStatusUpdateError,
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
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource status: failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test error`,
		},
		{
			name:      "reconciliation remove finalizer from istiocsr fails",
			setupMock: setupFinalizerRemovalError,
			requeue:   false,
			wantErr:   `failed to remove finalizers on "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with test client error`,
		},
		{
			name:      "reconciliation adding finalizer to istiocsr fails",
			setupMock: setupFinalizerAdditionError,
			requeue:   false,
			wantErr:   `failed to update "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with finalizers: failed to add finalizers on "istiocsr-test-ns/istiocsr-test-resource" istiocsr.openshift.operator.io with test client error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}
			r.ctrlClient = mock
			istiocsr := testIstioCSR()
			runReconcileTest(t, r, mock, istiocsr, tt.expectedStatusCondition, tt.requeue, tt.wantErr)
		})
	}
}

// Helper functions for TestProcessReconcileRequest

func setupProcessReconcileSuccessful(m *fakes.FakeCtrlClient) {
	setupBasicGetCalls(m, false)
	setupBasicExistsCalls(m)
	setupBasicCreateCalls(m)
}

func setupProcessReconcileWithExistingAnnotation(m *fakes.FakeCtrlClient) {
	setupBasicGetCalls(m, false)
	setupBasicExistsCalls(m)
	setupBasicCreateCalls(m)
}

func setupProcessReconcileListError(m *fakes.FakeCtrlClient) {
	m.ListCalls(func(ctx context.Context, list client.ObjectList, option ...client.ListOption) error {
		switch list.(type) {
		case *v1alpha1.IstioCSRList:
			return testError
		}
		return nil
	})
}

func setupProcessReconcileMultipleInstances(m *fakes.FakeCtrlClient) {
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
}

func setupProcessReconcileMultipleInstancesWithStatusError(m *fakes.FakeCtrlClient) {
	setupProcessReconcileMultipleInstances(m)
	m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, option ...client.SubResourceUpdateOption) error {
		switch obj.(type) {
		case *v1alpha1.IstioCSR:
			return testError
		}
		return nil
	})
}

func setupProcessReconcileMultipleInstancesWithAnnotationError(m *fakes.FakeCtrlClient) {
	setupProcessReconcileMultipleInstances(m)
	m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, option ...client.UpdateOption) error {
		switch obj.(type) {
		case *v1alpha1.IstioCSR:
			return testError
		}
		return nil
	})
}

func getIstioCSRWithProcessedAnnotation() *v1alpha1.IstioCSR {
	istiocsr := testIstioCSR()
	istiocsr.Annotations = map[string]string{
		controllerProcessedAnnotation: "true",
	}
	return istiocsr
}

func getIstioCSRWithRejectedAnnotation() *v1alpha1.IstioCSR {
	istiocsr := testIstioCSR()
	istiocsr.Annotations = map[string]string{
		controllerProcessingRejectedAnnotation: "true",
	}
	return istiocsr
}

func getIstioCSRForMultipleInstancesTest() *v1alpha1.IstioCSR {
	istiocsr := testIstioCSR()
	istiocsr.SetNamespace("istiocsr3")
	istiocsr.SetCreationTimestamp(metav1.NewTime(time.Now().Add(time.Second * 2)))
	return istiocsr
}

func runProcessReconcileTest(t *testing.T, r *Reconciler, mock *fakes.FakeCtrlClient, istiocsr *v1alpha1.IstioCSR, expectedStatusCondition []metav1.Condition, expectedAnnotations map[string]string, wantErr string) {
	_, err := r.processReconcileRequest(istiocsr,
		types.NamespacedName{Name: istiocsr.GetName(), Namespace: istiocsr.GetNamespace()})
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("processReconcileRequest() err: %v, wantErr: %v", err, wantErr)
	}
	if !reflect.DeepEqual(istiocsr.Annotations, expectedAnnotations) {
		t.Errorf("processReconcileRequest() annotations: %v, expectedAnnotations: %v", istiocsr.Annotations, expectedAnnotations)
	}
	for _, c1 := range istiocsr.Status.Conditions {
		for _, c2 := range expectedStatusCondition {
			if c1.Type == c2.Type {
				if c1.Status != c2.Status || c1.Reason != c2.Reason || c1.Message != c2.Message {
					t.Errorf("processReconcileRequest() condition: %+v, expectedStatusCondition: %+v", c1, c2)
				}
			}
		}
	}
}

func TestProcessReconcileRequest(t *testing.T) {
	// set the operand image env var
	t.Setenv("RELATED_IMAGE_CERT_MANAGER_ISTIOCSR", "registry.redhat.io/cert-manager/cert-manager-istio-csr-rhel9:latest")

	tests := []struct {
		name                    string
		getIstioCSR             func() *v1alpha1.IstioCSR
		setupMock               func(*fakes.FakeCtrlClient)
		expectedStatusCondition []metav1.Condition
		expectedAnnotations     map[string]string
		wantErr                 string
	}{
		{
			name:        "reconciliation successful",
			getIstioCSR: testIstioCSR,
			setupMock:   setupProcessReconcileSuccessful,
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
			name:        "reconciliation of existing istiocsr",
			getIstioCSR: getIstioCSRWithProcessedAnnotation,
			setupMock:   setupProcessReconcileWithExistingAnnotation,
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
			name:        "reconciliation of duplicate istiocsr with annotation present",
			getIstioCSR: getIstioCSRWithRejectedAnnotation,
			setupMock:   func(*fakes.FakeCtrlClient) {},
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
			name:        "validating multiple istiocsrs' fails while listing",
			getIstioCSR: testIstioCSR,
			setupMock:   setupProcessReconcileListError,
			wantErr:     `failed to fetch list of istiocsr resources: test client error`,
		},
		{
			name:        "validating multiple istiocsrs' successful by ignoring latest instance",
			getIstioCSR: getIstioCSRForMultipleInstancesTest,
			setupMock:   setupProcessReconcileMultipleInstances,
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
			name:        "validating multiple istiocsrs' failed to update status",
			getIstioCSR: getIstioCSRForMultipleInstancesTest,
			setupMock:   setupProcessReconcileMultipleInstancesWithStatusError,
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
			wantErr: `failed to update istiocsr3/istiocsr-test-resource status: failed to update istiocsr.openshift.operator.io "istiocsr3/istiocsr-test-resource" status: test client error`,
		},
		{
			name:        "validating multiple istiocsrs' failed to update annotations",
			getIstioCSR: getIstioCSRForMultipleInstancesTest,
			setupMock:   setupProcessReconcileMultipleInstancesWithAnnotationError,
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
			wantErr: `failed to update reject processing annotation to istiocsr3/istiocsr-test-resource: test client error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.setupMock != nil {
				tt.setupMock(mock)
			}
			r.ctrlClient = mock
			istiocsr := tt.getIstioCSR()
			runProcessReconcileTest(t, r, mock, istiocsr, tt.expectedStatusCondition, tt.expectedAnnotations, tt.wantErr)
		})
	}
}
