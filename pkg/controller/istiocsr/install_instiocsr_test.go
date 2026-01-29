package istiocsr

import (
	"context"
	"errors"
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

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

var (
	errLabelsMismatch = errors.New("labels mismatch in resource")
)

// Helper functions to reduce cognitive complexity

func setupGetCallsForIssuerAndSecret(m *fakes.FakeCtrlClient) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
		switch o := obj.(type) {
		case *certmanagerv1.Issuer:
			testIssuer().DeepCopyInto(o)
		case *corev1.Secret:
			testSecret().DeepCopyInto(o)
		}
		return nil
	})
}

func setupCreateCallsWithLabelValidation(m *fakes.FakeCtrlClient, labels map[string]string) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
		switch o := obj.(type) {
		case *appsv1.Deployment, *corev1.Service, *corev1.ServiceAccount:
			if !reflect.DeepEqual(o.GetLabels(), labels) {
				return fmt.Errorf("%w %v; got: %v, want: %v", errLabelsMismatch, o, o.GetLabels(), labels)
			}
		case *certmanagerv1.Certificate, *rbacv1.Role, *rbacv1.RoleBinding, *rbacv1.ClusterRole, *rbacv1.ClusterRoleBinding:
			expectedLabels := make(map[string]string)
			maps.Copy(expectedLabels, labels)
			expectedLabels[istiocsrNamespaceMappingLabelName] = testIstioCSRNamespace
			if !reflect.DeepEqual(o.GetLabels(), expectedLabels) {
				return fmt.Errorf("%w %v; got: %v, want: %v", errLabelsMismatch, o, o.GetLabels(), expectedLabels)
			}
		}
		return nil
	})
}

func setupCreateCallsForServiceAccountError(m *fakes.FakeCtrlClient) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
		if _, ok := obj.(*corev1.ServiceAccount); ok {
			return testError
		}
		return nil
	})
}

func setupCreateCallsForRoleError(m *fakes.FakeCtrlClient) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
		switch o := obj.(type) {
		case *rbacv1.Role:
			return testError
		case *rbacv1.ClusterRoleBinding:
			testClusterRoleBinding().DeepCopyInto(o)
		}
		return nil
	})
}

func setupCreateCallsForCertificateError(m *fakes.FakeCtrlClient) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, option ...client.CreateOption) error {
		switch o := obj.(type) {
		case *certmanagerv1.Certificate:
			return testError
		case *rbacv1.ClusterRoleBinding:
			testClusterRoleBinding().DeepCopyInto(o)
		}
		return nil
	})
}

func runReconcileDeploymentTest(t *testing.T, r *Reconciler, mock *fakes.FakeCtrlClient, istiocsr *v1alpha1.IstioCSR, wantErr string) {
	r.ctrlClient = mock
	err := r.reconcileIstioCSRDeployment(istiocsr, true)
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("reconcileIstioCSRDeployment() err: %v, wantErr: %v", err, wantErr)
	}
}

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
				setupGetCallsForIssuerAndSecret(m)
				setupCreateCallsWithLabelValidation(m, labels)
			},
		},
		{
			name: "istiocsr reconciliation fails with serviceaccount creation error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupCreateCallsForServiceAccountError(m)
			},
			wantErr: `failed to create istiocsr-test-ns/cert-manager-istio-csr serviceaccount resource: test client error`,
		},
		{
			name: "istiocsr reconciliation fails with role creation error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupCreateCallsForRoleError(m)
			},
			wantErr: `failed to create istio-test-ns/cert-manager-istio-csr role resource: test client error`,
		},
		{
			name: "istiocsr reconciliation fails with certificate creation error",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupCreateCallsForCertificateError(m)
			},
			wantErr: `failed to create istio-test-ns/istiod certificate resource: test client error`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			runReconcileDeploymentTest(t, r, mock, istiocsr, tt.wantErr)
		})
	}
}
