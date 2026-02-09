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

func TestCreateOrApplyCertificates(t *testing.T) {
	t.Run("error cases", func(t *testing.T) {
		testCreateOrApplyCertificates_Errors(t)
	})
	t.Run("successful and configuration cases", func(t *testing.T) {
		testCreateOrApplyCertificates_Successful(t)
	})
}

func testCreateOrApplyCertificates_Errors(t *testing.T) {
	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "reconciliation of certificate fails while checking if exists",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *certmanagerv1.Certificate:
						return false, errTestClient
					}
					return true, nil
				})
			},
			wantErr: `failed to check istio-test-ns/istiod certificate resource already exists: test client error`,
		},
		{
			name: "reconciliation of certificate fails while restoring to expected state",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *certmanagerv1.Certificate:
						cert := testCertificate()
						cert.SetLabels(map[string]string{"test": "test"})
						cert.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *certmanagerv1.Certificate:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to update istio-test-ns/istiod certificate resource: test client error`,
		},
		{
			name: "reconciliation of certificate creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *certmanagerv1.Certificate:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *certmanagerv1.Certificate:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create istio-test-ns/istiod certificate resource: test client error`,
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeySize and PrivateKeyAlgorithm is misconfigured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize = 2048
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm = "ECDSA"
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate certificate resource for creation in istiocsr-test-ns: failed to update certificate resource for istiocsr-test-ns/istiocsr-test-resource istiocsr deployment: failed to update certificate private key: certificate parameters PrivateKeySize and PrivateKeyAlgorithm do not comply`,
		},
	}
	runCertificateTests(t, tests)
}

func testCreateOrApplyCertificates_Successful(t *testing.T) {
	t.Run("existing certificate cases", func(t *testing.T) {
		testCreateOrApplyCertificates_Existing(t)
	})
	t.Run("configuration cases", func(t *testing.T) {
		testCreateOrApplyCertificates_Configuration(t)
	})
}

func testCreateOrApplyCertificates_Existing(t *testing.T) {
	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "reconciliation of certificate which already exists in expected state",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *certmanagerv1.Certificate:
						cert := testCertificate()
						cert.DeepCopyInto(o)
					}
					return true, nil
				})
			},
		},
		{
			name: "reconciliation of certificate creation fails",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
				m.ExistsCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *certmanagerv1.Certificate:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(_ context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *certmanagerv1.Certificate:
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to create istio-test-ns/istiod certificate resource: test client error`,
		},
	}
	runCertificateTests(t, tests)
}

func testCreateOrApplyCertificates_Configuration(t *testing.T) {
	tests := []struct {
		name    string
		preReq  func(*Reconciler, *fakes.FakeCtrlClient)
		wantErr string
	}{
		{
			name: "reconciliation of certificate when revisions are configured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.Istio.Revisions = []string{"", "basic"}
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate duration not configured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateDuration = nil
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate RenewBefore not configured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.CertificateRenewBefore = nil
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeyAlgorithm not configured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm = ""
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeySize not configured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize = 0
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "reconciliation of certificate when certificate PrivateKeySize and PrivateKeyAlgorithm is misconfigured",
			preReq: func(_ *Reconciler, m *fakes.FakeCtrlClient) {
				m.GetCalls(func(_ context.Context, _ types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *v1alpha1.IstioCSR:
						istiocsr := testIstioCSR()
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeySize = 2048
						istiocsr.Spec.IstioCSRConfig.IstiodTLSConfig.PrivateKeyAlgorithm = "ECDSA"
						istiocsr.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate certificate resource for creation in istiocsr-test-ns: failed to update certificate resource for istiocsr-test-ns/istiocsr-test-resource istiocsr deployment: failed to update certificate private key: certificate parameters PrivateKeySize and PrivateKeyAlgorithm do not comply`,
		},
	}
	runCertificateTests(t, tests)
}

// runCertificateTests is a helper function to run certificate test cases and reduce cyclomatic complexity.
func runCertificateTests(t *testing.T, tests []struct {
	name    string
	preReq  func(*Reconciler, *fakes.FakeCtrlClient)
	wantErr string
}) {
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := testReconciler(t)
			mock := &fakes.FakeCtrlClient{}
			if tt.preReq != nil {
				tt.preReq(r, mock)
			}
			r.ctrlClient = mock
			istiocsr := &v1alpha1.IstioCSR{}
			if err := r.Get(context.Background(), types.NamespacedName{
				Namespace: testIstioCSR().Namespace,
				Name:      testIstioCSR().Name,
			}, istiocsr); err != nil {
				t.Errorf("test error: %v", err)
			}
			err := r.createOrApplyCertificates(istiocsr, controllerDefaultResourceLabels, false)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyCertificates() err: %v, wantErr: %v", err, tt.wantErr)
			}
		})
	}
}
