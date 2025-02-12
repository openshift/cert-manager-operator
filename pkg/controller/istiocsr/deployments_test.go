package istiocsr

import (
	"context"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"sigs.k8s.io/controller-runtime/pkg/client"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"

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
						deployment := testDeployment()
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
			name: "deployment reconciliation fails as issuerRef does not exist",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
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
						deployment := testDeployment()
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
						deployment := testDeployment()
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
						issuer := testIssuer()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
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
						deployment := testDeployment()
						deployment.Labels = nil
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testClusterIssuer()
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
											Key:      "node",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"test"},
										},
									},
								},
							},
						},
					},
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "test",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"test"},
										},
									},
								},
								TopologyKey: "topology.kubernetes.io/zone",
							},
						},
					},
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "test",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"test"},
											},
										},
									},
									TopologyKey: "topology.kubernetes.io/zone",
								},
							},
						},
					},
				}
				i.Spec.IstioCSRConfig.Tolerations = []corev1.Toleration{
					{
						Key:      "type",
						Operator: corev1.TolerationOpEqual,
						Value:    "test",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				}
				i.Spec.IstioCSRConfig.NodeSelector = map[string]string{"type": "test"}
				i.Spec.IstioCSRConfig.Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("100Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("100Mi"),
					},
				}
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
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
				m.CreateCalls(func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					case *certmanagerv1.ClusterIssuer:
						issuer := testACMEIssuer()
						issuer.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testACMEIssuer()
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
						deployment := testDeployment()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *corev1.Secret:
						return apierrors.NewUnauthorized("no access")
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
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
						deployment := testDeployment()
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
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						return false, testError
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						configmap := testConfigMap()
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
						issuer := testIssuer()
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
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						return false, nil
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.Issuer:
						issuer := testIssuer()
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
		},
		{
			name: "deployment reconciliation with invalid toleration configuration",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
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
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testClusterIssuer()
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = certmanagerv1.ClusterIssuerKind
				i.Spec.IstioCSRConfig.Tolerations = []corev1.Toleration{
					{
						Operator: corev1.TolerationOpExists,
						Value:    "test",
						Effect:   corev1.TaintEffectNoSchedule,
					},
				}
			},
			wantErr: "failed to generate deployment resource for creation in istiocsr-test-ns: failed to update pod tolerations: spec.istioCSRConfig.tolerations[0].operator: Invalid value: core.Toleration{Key:\"\", Operator:\"Exists\", Value:\"test\", Effect:\"NoSchedule\", TolerationSeconds:(*int64)(nil)}: value must be empty when `operator` is 'Exists'",
		},
		{
			name: "deployment reconciliation with invalid nodeSelector configuration",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
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
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testClusterIssuer()
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = certmanagerv1.ClusterIssuerKind
				i.Spec.IstioCSRConfig.NodeSelector = map[string]string{"node/Label/2": "value2"}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update node selector: spec.istioCSRConfig.nodeSelector: Invalid value: "node/Label/2": a qualified name must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]') with an optional DNS subdomain prefix and '/' (e.g. 'example.com/MyName')`,
		},
		{
			name: "deployment reconciliation with invalid affinity configuration",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
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
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testClusterIssuer()
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
											Key:      "node",
											Operator: corev1.NodeSelectorOpIn,
										},
									},
								},
							},
						},
					},
					PodAffinity: &corev1.PodAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "test",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"test"},
										},
									},
								},
							},
						},
					},
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
								PodAffinityTerm: corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											{
												Key:      "test",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"test"},
											},
										},
									},
								},
							},
						},
					},
				}
			},
			wantErr: "failed to generate deployment resource for creation in istiocsr-test-ns: failed to update affinity rules: [spec.istioCSRConfig.affinity.nodeAffinity.requiredDuringSchedulingIgnoredDuringExecution.nodeSelectorTerms[0].matchExpressions[0].values: Required value: must be specified when `operator` is 'In' or 'NotIn', spec.istioCSRConfig.affinity.podAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey: Required value: can not be empty, spec.istioCSRConfig.affinity.podAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey: Invalid value: \"\": name part must be non-empty, spec.istioCSRConfig.affinity.podAffinity.requiredDuringSchedulingIgnoredDuringExecution[0].topologyKey: Invalid value: \"\": name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]'), spec.istioCSRConfig.affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.topologyKey: Required value: can not be empty, spec.istioCSRConfig.affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.topologyKey: Invalid value: \"\": name part must be non-empty, spec.istioCSRConfig.affinity.podAntiAffinity.preferredDuringSchedulingIgnoredDuringExecution[0].podAffinityTerm.topologyKey: Invalid value: \"\": name part must consist of alphanumeric characters, '-', '_' or '.', and must start and end with an alphanumeric character (e.g. 'MyName',  or 'my.name',  or '123-abc', regex used for validation is '([A-Za-z0-9][-A-Za-z0-9_.]*)?[A-Za-z0-9]')]",
		},
		{
			name: "deployment reconciliation with invalid resource requirement configuration",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
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
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *certmanagerv1.ClusterIssuer:
						issuer := testClusterIssuer()
						issuer.DeepCopyInto(o)
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Spec.IstioCSRConfig.CertManager.IssuerRef.Kind = certmanagerv1.ClusterIssuerKind
				i.Spec.IstioCSRConfig.Resources = corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("100Mi"),
						"test":                resource.MustParse("100.0"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("100Mi"),
					},
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update resource requirements: [spec.istioCSRConfig.resources.requests[test]: Invalid value: test: must be a standard resource type or fully qualified, spec.istioCSRConfig.resources.requests[test]: Invalid value: test: must be a standard resource for containers]`,
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
			istiocsr := testIstioCSR()
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
