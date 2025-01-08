package istiocsr

import (
	"context"
	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr/fakes"
)

func TestCreateOrApplyDeployments(t *testing.T) {
	tests := []struct {
		name           string
		preReq         func(*Reconciler, *fakes.FakeCtrlClient)
		updateIstioCSR func(*v1alpha1.IstioCSR)
		skipEnvVar     bool
		wantErr        string
	}{
		{
			name: "deployment reconciliation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
			},
		},
		{
			name: "deployment reconciliation fails as IstioCSRConfig is empty",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig = nil
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: not creating deployment resource: istiocsr-test-ns/istiocsr-test-resource spec.IstioCSRConfig is empty`,
		},
		{
			name: "deployment reconciliation fails as issuerRef does not exist",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						return apierrors.NewNotFound(certmanagerv1.Resource("issuers"), testResourcesName)
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = certmanagerv1.ClusterIssuerKind
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to verify issuer in istiocsr-test-ns/istiocsr-test-resource: failed to fetch issuer: failed to fetch "istio-test-ns/istiocsr-test-resource" issuer: issuers.cert-manager.io "istiocsr-test-resource" not found`,
		},
		{
			name: "deployment reconciliation fails as image env var is empty",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			skipEnvVar: true,
			wantErr:    `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update image istiocsr-test-ns/istiocsr-test-resource: RELATED_IMAGE_CERT_MANAGER_ISTIOCSR environment variable with istiocsr image not set`,
		},
		{
			name: "deployment reconciliation fails while creating configmap",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						return false, nil
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, _ ...client.CreateOption) error {
					switch obj.(type) {
					case *corev1.ConfigMap:
						return testError
					}
					return nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to create CA ConfigMap: failed to create istiocsr-test-ns/cert-manager-istio-csr-issuer-ca-copy configmap resource: test client error`,
		},
		{
			name: "deployment reconciliation updating volume successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
			},
		},
		{
			name: "deployment reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch obj.(type) {
					case *appsv1.Deployment:
						return false, testError
					}
					return true, nil
				})
			},
			wantErr: `failed to check istiocsr-test-ns/cert-manager-istio-csr deployment resource already exists: test client error`,
		},
		{
			name: "deployment reconciliation failed while restoring to desired state",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.Labels = nil
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *appsv1.Deployment:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/cert-manager-istio-csr deployment resource: test client error`,
		},
		{
			name: "deployment reconciliation with user custom config successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testClusterIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = certmanagerv1.ClusterIssuerKind
				i.Spec.IstioCSRConfig.Affinity = &corev1.Affinity{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      testResourcesName,
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{testResourcesName},
										},
									},
								},
							},
						},
					},
				}
				i.Spec.IstioCSRConfig.Tolerations = []corev1.Toleration{
					{
						Operator: corev1.TolerationOpExists,
					},
				}
				i.Spec.IstioCSRConfig.NodeSelector = map[string]string{"type": "test"}
			},
		},
		{
			name: "deployment reconciliation fails while updating image in istiocsr status",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						return false, nil
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return nil
				})
				m.StatusUpdateCalls(func(ctx context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
					switch obj.(type) {
					case *v1alpha1.IstioCSR:
						return testError
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with image info: failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
		},
		{
			name: "deployment reconciliation fails as invalid kind in issuerRef",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = "invalid"
			},
			wantErr: "failed to generate deployment resource for creation in istiocsr-test-ns: failed to verify issuer in istiocsr-test-ns/istiocsr-test-resource: spec.istioCSRConfig.certManager.issuerRef.kind can be anyof `clusterissuer` or `issuer`, configured: issuer: invalid issuerRef config",
		},
		{
			name: "deployment reconciliation fails as invalid group in issuerRef",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Group = "invalid"
			},
			wantErr: "failed to generate deployment resource for creation in istiocsr-test-ns: failed to verify issuer in istiocsr-test-ns/istiocsr-test-resource: spec.istioCSRConfig.certManager.issuerRef.group can be only `cert-manager.io`, configured: invalid: invalid issuerRef config",
		},
		{
			name: "deployment reconciliation fails as unsupported ACME issuer is used",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					case *certmanagerv1.ClusterIssuer:
						issuer := testACMEIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testACMEIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = certmanagerv1.ClusterIssuerKind
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to verify issuer in istiocsr-test-ns/istiocsr-test-resource: spec.istioCSRConfig.certManager.issuerRef uses unsupported ACME issuer: invalid issuerRef config`,
		},
		{
			name: "deployment reconciliation while fetching issuer",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch obj.(type) {
					case *certmanagerv1.Issuer:
						return apierrors.NewUnauthorized("no access")
					}
					return nil
				})
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to verify issuer in istiocsr-test-ns/istiocsr-test-resource: failed to fetch issuer: failed to fetch "istio-test-ns/istiocsr-test-resource" issuer: no access`,
		},
		{
			name: "deployment reconciliation fails while fetching secret referenced in issuer",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *corev1.Secret:
						return apierrors.NewUnauthorized("no access")
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to create CA ConfigMap: failed to fetch secret in issuer: no access`,
		},
		{
			name: "deployment reconciliation fails while updating labels on secret referenced in issuer",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *corev1.Secret:
						return apierrors.NewUnauthorized("no access")
					}
					return nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to create CA ConfigMap: failed to update  secret with custom watch label: no access`,
		},
		{
			name: "deployment reconciliation fails while checking configmap exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						return false, testError
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to create CA ConfigMap: failed to check if CA configmap exists: test client error`,
		},
		{
			name: "deployment reconciliation fails while updating configmap to desired state",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap(t)
						configmap.Labels = nil
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *corev1.ConfigMap:
						return testError
					}
					return nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to create CA ConfigMap: failed to update istiocsr-test-ns/cert-manager-istio-csr-issuer-ca-copy configmap resource: test client error`,
		},
		{
			name: "deployment reconciliation configmap creation successful",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment(t)
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						return false, nil
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer(t)
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
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
			if tt.updateIstioCSR != nil {
				tt.updateIstioCSR(istiocsr)
			}
			if !tt.skipEnvVar {
				t.Setenv("RELATED_IMAGE_CERT_MANAGER_ISTIOCSR", image)
			}
			err := r.createOrApplyDeployments(istiocsr, controllerDefaultResourceLabels, false)
			if (tt.wantErr != "" || err != nil) && (err == nil || err.Error() != tt.wantErr) {
				t.Errorf("createOrApplyDeployments() err: %v, wantErr: %v", err, tt.wantErr)
			}
			if tt.wantErr == "" {
				if istiocsr.Status.IstioCSRImage != image {
					t.Errorf("createOrApplyDeployments() got: %v, want: %v", istiocsr.Status.IstioCSRImage, image)
				}
			}
		})
	}
}
