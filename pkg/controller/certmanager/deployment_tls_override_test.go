package certmanager

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configv1informer "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1lister "github.com/openshift/client-go/config/listers/config/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

type testAPIServerInformer struct {
	lister configv1lister.APIServerLister
}

func (f *testAPIServerInformer) Informer() cache.SharedIndexInformer {
	panic("not implemented")
}

func (f *testAPIServerInformer) Lister() configv1lister.APIServerLister {
	return f.lister
}

type testAPIServerLister struct {
	apiServer *configv1.APIServer
}

func (f *testAPIServerLister) List(selector labels.Selector) ([]*configv1.APIServer, error) {
	if f.apiServer != nil {
		return []*configv1.APIServer{f.apiServer}, nil
	}
	return nil, nil
}

func (f *testAPIServerLister) Get(name string) (*configv1.APIServer, error) {
	if f.apiServer != nil && f.apiServer.Name == name {
		return f.apiServer, nil
	}
	return nil, fmt.Errorf("apiserver.config.openshift.io %q not found", name)
}

func applyClusterTLSAndUnsupportedOverrides(
	deployment *appsv1.Deployment,
	apiServer *configv1.APIServer,
	overrides *v1alpha1.UnsupportedConfigOverrides,
) error {
	clusterTLSHook := common.WithClusterTLSProfileFromAPIServer(&testAPIServerInformer{
		lister: &testAPIServerLister{apiServer: apiServer},
	})
	if err := clusterTLSHook(&operatorv1.OperatorSpec{}, deployment); err != nil {
		return err
	}

	raw, err := json.Marshal(overrides)
	if err != nil {
		return err
	}
	return withUnsupportedArgsOverrideHook(&operatorv1.OperatorSpec{
		UnsupportedConfigOverrides: runtime.RawExtension{Raw: raw},
	}, deployment)
}

func deploymentArgMap(args []string) map[string]string {
	m := make(map[string]string, len(args))
	common.ParseArgMap(m, args)
	return m
}

func TestUnsupportedConfigOverridesWinOverClusterTLSProfile(t *testing.T) {
	modernProfile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileModernType}
	intermediateProfile := &configv1.TLSSecurityProfile{Type: configv1.TLSProfileIntermediateType}
	strictAdherence := configv1.TLSAdherencePolicyStrictAllComponents

	tests := []struct {
		name           string
		deploymentName string
		baseArgs       []string
		tlsProfile     *configv1.TLSSecurityProfile
		overrides      *v1alpha1.UnsupportedConfigOverrides
		wantTLSArgs    map[string]string
		wantAbsentKeys []string
	}{
		{
			name:           "webhook unsupported TLS args override cluster Intermediate profile",
			deploymentName: certmanagerWebhookDeployment,
			baseArgs:       []string{"--v=2", "--secure-port=10250"},
			tlsProfile:     intermediateProfile,
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: []string{
						"--tls-min-version=VersionTLS10",
						"--tls-cipher-suites=CUSTOM_MAIN",
						"--metrics-tls-min-version=VersionTLS10",
						"--metrics-tls-cipher-suites=CUSTOM_METRICS",
					},
				},
			},
			wantTLSArgs: map[string]string{
				"--tls-min-version":           "VersionTLS10",
				"--tls-cipher-suites":         "CUSTOM_MAIN",
				"--metrics-tls-min-version":   "VersionTLS10",
				"--metrics-tls-cipher-suites": "CUSTOM_METRICS",
			},
		},
		{
			name:           "controller unsupported metrics TLS args override cluster Intermediate profile",
			deploymentName: certmanagerControllerDeployment,
			baseArgs:       []string{"--v=2", "--leader-election-namespace=kube-system"},
			tlsProfile:     intermediateProfile,
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Controller: v1alpha1.UnsupportedConfigOverridesForCertManagerController{
					Args: []string{
						"--metrics-tls-min-version=VersionTLS10",
						"--metrics-tls-cipher-suites=CUSTOM_METRICS",
					},
				},
			},
			wantTLSArgs: map[string]string{
				"--metrics-tls-min-version":   "VersionTLS10",
				"--metrics-tls-cipher-suites": "CUSTOM_METRICS",
			},
		},
		{
			name:           "cainjector unsupported metrics TLS args override cluster Intermediate profile",
			deploymentName: certmanagerCAinjectorDeployment,
			baseArgs:       []string{"--v=2", "--leader-election-namespace=kube-system"},
			tlsProfile:     intermediateProfile,
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: []string{
						"--metrics-tls-min-version=VersionTLS10",
						"--metrics-tls-cipher-suites=CUSTOM_METRICS",
					},
				},
			},
			wantTLSArgs: map[string]string{
				"--metrics-tls-min-version":   "VersionTLS10",
				"--metrics-tls-cipher-suites": "CUSTOM_METRICS",
			},
		},
		{
			name:           "webhook unsupported sets cipher suites when cluster profile is TLS 1.3",
			deploymentName: certmanagerWebhookDeployment,
			baseArgs:       []string{"--v=2", "--secure-port=10250"},
			tlsProfile:     modernProfile,
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: []string{
						"--tls-cipher-suites=CUSTOM_MAIN",
						"--metrics-tls-cipher-suites=CUSTOM_METRICS",
					},
				},
			},
			wantTLSArgs: map[string]string{
				"--tls-min-version":           "VersionTLS13",
				"--metrics-tls-min-version":   "VersionTLS13",
				"--tls-cipher-suites":         "CUSTOM_MAIN",
				"--metrics-tls-cipher-suites": "CUSTOM_METRICS",
			},
		},
		{
			name:           "webhook partial unsupported min TLS override keeps other cluster TLS args",
			deploymentName: certmanagerWebhookDeployment,
			baseArgs:       []string{"--v=2", "--secure-port=10250"},
			tlsProfile:     modernProfile,
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: []string{
						"--tls-min-version=VersionTLS12",
					},
				},
			},
			wantTLSArgs: map[string]string{
				"--tls-min-version":         "VersionTLS12",
				"--metrics-tls-min-version": "VersionTLS13",
			},
			wantAbsentKeys: []string{
				"--tls-cipher-suites",
				"--metrics-tls-cipher-suites",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: tt.deploymentName},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{
								Name: "cert-manager",
								Args: append([]string(nil), tt.baseArgs...),
							}},
						},
					},
				},
			}

			apiServer := &configv1.APIServer{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1.APIServerSpec{
					TLSSecurityProfile: tt.tlsProfile,
					TLSAdherence:       strictAdherence,
				},
			}

			err := applyClusterTLSAndUnsupportedOverrides(deployment, apiServer, tt.overrides)
			require.NoError(t, err)

			got := deploymentArgMap(deployment.Spec.Template.Spec.Containers[0].Args)
			for key, want := range tt.wantTLSArgs {
				require.Equal(t, want, got[key], "arg %q", key)
			}
			for _, key := range tt.wantAbsentKeys {
				_, present := got[key]
				require.False(t, present, "arg %q should not be set", key)
			}
		})
	}
}

var _ configv1informer.APIServerInformer = (*testAPIServerInformer)(nil)
