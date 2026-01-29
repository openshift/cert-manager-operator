package istiocsr

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

// Helper functions to reduce cognitive complexity

func setupGetCallsForIstioCSR(m *fakes.FakeCtrlClient, modifyIstioCSR func(*v1alpha1.IstioCSR)) {
	m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
		if o, ok := obj.(*v1alpha1.IstioCSR); ok {
			istiocsr := testIstioCSR()
			if modifyIstioCSR != nil {
				modifyIstioCSR(istiocsr)
			}
			istiocsr.DeepCopyInto(o)
		}
		return nil
	})
}

func setupExistsCallsForCertificate(m *fakes.FakeCtrlClient, exists bool, err error, modifyCert func(*certmanagerv1.Certificate)) {
	m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
		if cert, ok := obj.(*certmanagerv1.Certificate); ok {
			if modifyCert != nil {
				modifyCert(cert)
			}
			return exists, err
		}
		return true, nil
	})
}

func setupCreateCallsForCertificate(m *fakes.FakeCtrlClient, err error) {
	m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
		if _, ok := obj.(*certmanagerv1.Certificate); ok {
			return err
		}
		return nil
	})
}

func setupUpdateWithRetryCallsForCertificate(m *fakes.FakeCtrlClient, err error) {
	m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
		if _, ok := obj.(*certmanagerv1.Certificate); ok {
			return err
		}
		return nil
	})
}

func runCertificateTest(t *testing.T, r *Reconciler, mock *fakes.FakeCtrlClient, wantErr string) {
	r.ctrlClient = mock
	istiocsr := &v1alpha1.IstioCSR{}
	if err := r.Get(context.Background(), types.NamespacedName{
		Namespace: testIstioCSR().Namespace,
		Name:      testIstioCSR().Name,
	}, istiocsr); err != nil {
		t.Errorf("test error: %v", err)
		return
	}
	err := r.createOrApplyCertificates(istiocsr, controllerDefaultResourceLabels, false)
	if (wantErr != "" || err != nil) && (err == nil || err.Error() != wantErr) {
		t.Errorf("createOrApplyCertificates() err: %v, wantErr: %v", err, wantErr)
	}
}

func TestCreateOrApplyCertificates(t *testing.T) {
	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "reconciliation of certificate fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, nil)
				setupExistsCallsForCertificate(m, false, testError, nil)
			},
			wantErr: `failed to check istio-test-ns/istiod certificate resource already exists: test client error`,
		},
		{
			name: "reconciliation of certificate fails while restoring to expected state",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, nil)
				setupExistsCallsForCertificate(m, true, nil, func(cert *certmanagerv1.Certificate) {
					testCert := testCertificate()
					testCert.SetLabels(map[string]string{"test": "test"})
					testCert.DeepCopyInto(cert)
				})
				setupUpdateWithRetryCallsForCertificate(m, testError)
			},
			wantErr: `failed to update istio-test-ns/istiod certificate resource: test client error`,
		},
		{
			name: "reconciliation of certificate which already exists in expected state",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, nil)
				setupExistsCallsForCertificate(m, true, nil, func(cert *certmanagerv1.Certificate) {
					testCertificate().DeepCopyInto(cert)
				})
			},
		},
		{
			name: "reconciliation of certificate creation fails",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, nil)
				setupExistsCallsForCertificate(m, false, nil, nil)
				setupCreateCallsForCertificate(m, testError)
			},
			wantErr: `failed to create istio-test-ns/istiod certificate resource: test client error`,
		},
		{
			name: "reconciliation of certificate when revisions are configured",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, func(istiocsr *v1alpha1.IstioCSR) {
					istiocsr.Spec.IstioCSRConfig.Istio.Revisions = []string{"", "basic"}
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate duration not configured",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, func(istiocsr *v1alpha1.IstioCSR) {
					istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDuration = nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate RenewBefore not configured",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, func(istiocsr *v1alpha1.IstioCSR) {
					istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateRenewBefore = nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeyAlgorithm not configured",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, func(istiocsr *v1alpha1.IstioCSR) {
					istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm = ""
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeySize not configured",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, func(istiocsr *v1alpha1.IstioCSR) {
					istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize = 0
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeySize and PrivateKeyAlgorithm is misconfigured",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				setupGetCallsForIstioCSR(m, func(istiocsr *v1alpha1.IstioCSR) {
					istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize = 2048
					istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm = "ECDSA"
				})
			},
			wantErr: `failed to generate certificate resource for creation in istiocsr-test-ns: failed to update certificate resource for istiocsr-test-ns/istiocsr-test-resource istiocsr deployment: certificate parameters PrivateKeySize and PrivateKeyAlgorithm do not comply`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			runCertificateTest(t, r, mock, tt.wantErr)
		})
	}
}
