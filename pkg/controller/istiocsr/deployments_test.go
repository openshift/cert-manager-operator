package istiocsr

import (
	"context"
	"fmt"
	"reflect"
	"slices"
	"strings"
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
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
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
						return errTestClient
					}
					return nil
				})
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
			},
		},
		{
			name: "deployment reconciliation fails while checking if exists",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						return false, errTestClient
					case *corev1.ConfigMap:
						configmap := testConfigMap()
						configmap.DeepCopyInto(o)
					}
					return true, nil
				})
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
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *appsv1.Deployment:
						return errTestClient
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
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
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
						return errTestClient
					}
					return nil
				})
			},
			wantErr: `failed to update istiocsr-test-ns/istiocsr-test-resource istiocsr status with image info: failed to update status for "istiocsr-test-ns/istiocsr-test-resource": failed to update istiocsr.openshift.operator.io "istiocsr-test-ns/istiocsr-test-resource" status: test client error`,
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
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to create CA ConfigMap: failed to update  resource with watch label: no access`,
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
						return false, errTestClient
					}
					return true, nil
				})
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
						return errTestClient
					}
					return nil
				})
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
					case *corev1.Secret:
						secret := testSecret()
						secret.DeepCopyInto(o)
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
			wantErr: "failed to generate deployment resource for creation in istiocsr-test-ns: failed to update pod tolerations: spec.istioCSRConfig.tolerations[0].operator: Invalid value: \"test\": value must be empty when `operator` is 'Exists'",
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
		{
			name: "deployment reconciliation successful with CA certificate ConfigMap",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						// Return false for the copied ConfigMap to simulate creation
						return false, nil
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *corev1.ConfigMap:
						if ns.Name == "ca-cert-test" {
							configMap := testCACertificateConfigMap()
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
				m.UpdateWithRetryReturns(nil)
				m.CreateReturns(nil)
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "ca-cert-test",
					Key:  "ca-cert.pem",
				}
			},
		},
		{
			name: "deployment reconciliation successful with CA certificate ConfigMap in custom namespace",
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
					case *corev1.ConfigMap:
						if ns.Name == "ca-cert-test" && ns.Namespace == "custom-namespace" {
							configMap := testCACertificateConfigMap()
							configMap.Namespace = "custom-namespace"
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
				m.UpdateWithRetryReturns(nil)
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name:      "ca-cert-test",
					Namespace: "custom-namespace",
					Key:       "ca-cert.pem",
				}
			},
		},
		{
			name: "deployment reconciliation fails with missing CA certificate ConfigMap",
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
					case *corev1.ConfigMap:
						if ns.Name == "ca-cert-test" {
							return apierrors.NewNotFound(corev1.Resource("configmaps"), "ca-cert-test")
						}
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "ca-cert-test",
					Key:  "ca-cert.pem",
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to validate and mount CA certificate ConfigMap: failed to fetch CA certificate ConfigMap istiocsr-test-ns/ca-cert-test: configmaps "ca-cert-test" not found`,
		},
		{
			name: "deployment reconciliation fails with missing key in CA certificate ConfigMap",
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
					case *corev1.ConfigMap:
						if ns.Name == "ca-cert-test" {
							configMap := testCACertificateConfigMap()
							// Remove the expected key
							delete(configMap.Data, "ca-cert.pem")
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "ca-cert-test",
					Key:  "ca-cert.pem",
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to validate and mount CA certificate ConfigMap: invalid CA certificate ConfigMap istiocsr-test-ns/ca-cert-test: key "ca-cert.pem" not found in ConfigMap istiocsr-test-ns/ca-cert-test`,
		},
		{
			name: "deployment reconciliation fails with invalid PEM data in CA certificate ConfigMap",
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
					case *corev1.ConfigMap:
						if ns.Name == "ca-cert-test" {
							configMap := testCACertificateConfigMap()
							// Set invalid PEM data
							configMap.Data["ca-cert.pem"] = "invalid-pem-data"
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "ca-cert-test",
					Key:  "ca-cert.pem",
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to validate and mount CA certificate ConfigMap: invalid PEM data in CA certificate ConfigMap istiocsr-test-ns/ca-cert-test key "ca-cert.pem": no valid PEM data found`,
		},

		{
			name: "deployment reconciliation fails while updating watch label on CA certificate ConfigMap",
			preReq: func(r *Reconciler, m *fakes.FakeCtrlClient) {
				m.ExistsCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) (bool, error) {
					switch o := obj.(type) {
					case *appsv1.Deployment:
						deployment := testDeployment()
						deployment.DeepCopyInto(o)
					case *corev1.ConfigMap:
						// Return false for the copied ConfigMap to simulate creation
						return false, nil
					}
					return true, nil
				})
				m.GetCalls(func(ctx context.Context, ns types.NamespacedName, obj client.Object) error {
					switch o := obj.(type) {
					case *corev1.ConfigMap:
						if ns.Name == "watch-label-fail-test" {
							configMap := testCACertificateConfigMap()
							configMap.Name = "watch-label-fail-test"
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
				m.UpdateWithRetryCalls(func(ctx context.Context, obj client.Object, _ ...client.UpdateOption) error {
					switch obj.(type) {
					case *corev1.ConfigMap:
						// Fail when trying to update the source ConfigMap with watch label
						if obj.GetName() == "watch-label-fail-test" {
							return apierrors.NewUnauthorized("no access to update watch label")
						}
					}
					return nil
				})
				m.CreateReturns(nil)
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "watch-label-fail-test",
					Key:  "ca-cert.pem",
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to validate and mount CA certificate ConfigMap: failed to update watch label on CA certificate ConfigMap istiocsr-test-ns/watch-label-fail-test: failed to update watch-label-fail-test resource with watch label: no access to update watch label`,
		},

		{
			name: "deployment reconciliation fails with non-CA certificate in ConfigMap",
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
					case *corev1.ConfigMap:
						if ns.Name == "non-ca-cert-test" {
							configMap := testNonCACertificateConfigMap()
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "non-ca-cert-test",
					Key:  "ca-cert.pem",
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to validate and mount CA certificate ConfigMap: invalid PEM data in CA certificate ConfigMap istiocsr-test-ns/non-ca-cert-test key "ca-cert.pem": certificate is not a CA certificate`,
		},

		{
			name: "deployment reconciliation fails with certificate missing KeyUsageCertSign in ConfigMap",
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
					case *corev1.ConfigMap:
						if ns.Name == "ca-without-certsign-test" {
							configMap := testCertificateWithoutCertSignConfigMap()
							configMap.DeepCopyInto(o)
						}
					}
					return nil
				})
			},
			updateIstioCSR: func(i *v1alpha1.IstioCSR) {
				i.Status.IstioCSRImage = image
				i.Spec.IstioCSRConfig.CertManager.IstioCACertificate = &v1alpha1.ConfigMapReference{
					Name: "ca-without-certsign-test",
					Key:  "ca-cert.pem",
				}
			},
			wantErr: `failed to generate deployment resource for creation in istiocsr-test-ns: failed to update volume istiocsr-test-ns/istiocsr-test-resource: failed to validate and mount CA certificate ConfigMap: invalid PEM data in CA certificate ConfigMap istiocsr-test-ns/ca-without-certsign-test key "ca-cert.pem": certificate does not have Certificate Sign key usage`,
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

func TestUpdateArgList(t *testing.T) {
	tests := []struct {
		name            string
		updateIstioCSR  func(*v1alpha1.IstioCSR)
		expectedArgs    map[string]string // key is arg name (without --), value is expected value
		notExpectedArgs []string          // arg names (without --) that should NOT be present
	}{
		{
			name: "clusterID not provided should default to Kubernetes",
			updateIstioCSR: func(istiocsr *v1alpha1.IstioCSR) {
				// Server config is nil, so clusterID should default
			},
			expectedArgs: map[string]string{
				"cluster-id": "Kubernetes",
			},
		},
		{
			name: "clusterID empty string should default to Kubernetes",
			updateIstioCSR: func(istiocsr *v1alpha1.IstioCSR) {
				istiocsr.Spec.IstioCSRConfig.Server = &v1alpha1.ServerConfig{
					ClusterID: "",
				}
			},
			expectedArgs: map[string]string{
				"cluster-id": "Kubernetes",
			},
		},
		{
			name: "clusterID provided should use custom value",
			updateIstioCSR: func(istiocsr *v1alpha1.IstioCSR) {
				istiocsr.Spec.IstioCSRConfig.Server = &v1alpha1.ServerConfig{
					ClusterID: "cluster-123_dev.local",
				}
			},
			expectedArgs: map[string]string{
				"cluster-id": "cluster-123_dev.local",
			},
		},
		{
			name: "istioDataPlaneNamespaceSelector not provided should not include argument",
			notExpectedArgs: []string{
				"configmap-namespace-selector",
			},
		},
		{
			name: "istioDataPlaneNamespaceSelector provided should include argument",
			updateIstioCSR: func(istiocsr *v1alpha1.IstioCSR) {
				istiocsr.Spec.IstioCSRConfig.IstioDataPlaneNamespaceSelector = "cert-manager.io/test-ca-injection=enabled"
			},
			expectedArgs: map[string]string{
				"configmap-namespace-selector": "cert-manager.io/test-ca-injection=enabled",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := testDeployment()
			istiocsr := testIstioCSR()
			if tt.updateIstioCSR != nil {
				tt.updateIstioCSR(istiocsr)
			}

			updateArgList(deployment, istiocsr)

			// Find the istio-csr container and check its arguments
			var containerArgs []string
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == istiocsrContainerName {
					containerArgs = container.Args
					break
				}
			}

			if len(containerArgs) == 0 {
				t.Fatalf("Expected container args to be set, but got empty args")
			}

			// Verify each expected argument
			for argName, expectedValue := range tt.expectedArgs {
				expectedArg := fmt.Sprintf("--%s=%s", argName, expectedValue)
				if !containsArg(containerArgs, expectedArg) {
					t.Errorf("Expected to find argument %q in container args, but it was not found. Args: %v", expectedArg, containerArgs)
				}
			}

			// Verify arguments that should NOT be present
			for _, argName := range tt.notExpectedArgs {
				for _, arg := range containerArgs {
					if strings.HasPrefix(arg, fmt.Sprintf("--%s=", argName)) {
						t.Errorf("Expected NOT to find argument %q in container args. Args: %v", arg, containerArgs)
						break
					}
				}
			}
		})
	}
}

// containsArg checks if the given argument is present in the args list.
func containsArg(args []string, targetArg string) bool {
	return slices.Contains(args, targetArg)
}

func TestUpdateVolumeWithIssuerCA(t *testing.T) {
	defaultMode := int32(420)
	expectedCAVolume := corev1.Volume{
		Name: "root-ca",
		VolumeSource: corev1.VolumeSource{
			ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{
					Name: "cert-manager-istio-csr-issuer-ca-copy",
				},
				Items: []corev1.KeyToPath{
					{
						Key:  "ca.crt",
						Path: "ca.crt",
						Mode: &defaultMode,
					},
				},
				DefaultMode: &defaultMode,
			},
		},
	}
	expectedCAMount := corev1.VolumeMount{
		Name:      "root-ca",
		MountPath: "/var/run/configmaps/istio-csr",
		ReadOnly:  true,
	}

	tests := []struct {
		name                 string
		existingVolumes      []corev1.Volume
		existingVolumeMounts []corev1.VolumeMount
		expectedVolumes      []corev1.Volume
		expectedMounts       []corev1.VolumeMount
	}{
		{
			name:            "add CA volume when no volumes exist",
			expectedVolumes: []corev1.Volume{expectedCAVolume},
			expectedMounts:  []corev1.VolumeMount{expectedCAMount},
		},
		{
			name: "add CA volume preserving other volumes",
			existingVolumes: []corev1.Volume{
				{Name: "other-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
			},
			existingVolumeMounts: []corev1.VolumeMount{
				{Name: "other-volume", MountPath: "/other"},
			},
			expectedVolumes: []corev1.Volume{
				{Name: "other-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				expectedCAVolume,
			},
			expectedMounts: []corev1.VolumeMount{
				{Name: "other-volume", MountPath: "/other"},
				expectedCAMount,
			},
		},
		{
			name: "update CA volume preserving others",
			existingVolumes: []corev1.Volume{
				{Name: "volume-1", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				{Name: "root-ca", VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{Name: "old-configmap"},
					},
				}},
			},
			existingVolumeMounts: []corev1.VolumeMount{
				{Name: "volume-1", MountPath: "/path1"},
				{Name: "root-ca", MountPath: "/old/path"},
			},
			expectedVolumes: []corev1.Volume{
				{Name: "volume-1", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
				expectedCAVolume,
			},
			expectedMounts: []corev1.VolumeMount{
				{Name: "volume-1", MountPath: "/path1"},
				expectedCAMount,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := testDeployment()
			deployment.Spec.Template.Spec.Volumes = tt.existingVolumes

			// Set existing volume mounts on istio-csr container
			for i, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == istiocsrContainerName {
					deployment.Spec.Template.Spec.Containers[i].VolumeMounts = tt.existingVolumeMounts
					break
				}
			}

			updateVolumeWithIssuerCA(deployment)

			// Verify volumes match expected
			if !reflect.DeepEqual(deployment.Spec.Template.Spec.Volumes, tt.expectedVolumes) {
				t.Errorf("volumes mismatch:\ngot: %+v\nwant: %+v", deployment.Spec.Template.Spec.Volumes, tt.expectedVolumes)
			}

			// Find istio-csr container and verify mounts
			var containerMounts []corev1.VolumeMount
			for _, container := range deployment.Spec.Template.Spec.Containers {
				if container.Name == istiocsrContainerName {
					containerMounts = container.VolumeMounts
					break
				}
			}

			// Verify mounts match expected
			if !reflect.DeepEqual(containerMounts, tt.expectedMounts) {
				t.Errorf("volume mounts mismatch:\ngot: %+v\nwant: %+v", containerMounts, tt.expectedMounts)
			}
		})
	}
}
