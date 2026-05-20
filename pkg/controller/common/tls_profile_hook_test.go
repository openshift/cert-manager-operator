package common

import (
	"fmt"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configv1informer "github.com/openshift/client-go/config/informers/externalversions/config/v1"
	configv1lister "github.com/openshift/client-go/config/listers/config/v1"
)

// fakeAPIServerInformer implements a minimal APIServerInformer for testing.
type fakeAPIServerInformer struct {
	lister configv1lister.APIServerLister
}

func (f *fakeAPIServerInformer) Informer() cache.SharedIndexInformer {
	panic("not implemented")
}

func (f *fakeAPIServerInformer) Lister() configv1lister.APIServerLister {
	return f.lister
}

// fakeAPIServerLister implements APIServerLister for testing.
type fakeAPIServerLister struct {
	apiServer *configv1.APIServer
}

func (f *fakeAPIServerLister) List(selector labels.Selector) (ret []*configv1.APIServer, err error) {
	if f.apiServer != nil {
		return []*configv1.APIServer{f.apiServer}, nil
	}
	return nil, nil
}

func (f *fakeAPIServerLister) Get(name string) (*configv1.APIServer, error) {
	if f.apiServer != nil && f.apiServer.Name == name {
		return f.apiServer, nil
	}
	return nil, fmt.Errorf("apiserver.config.openshift.io %q not found", name)
}

func newFakeAPIServerInformer(apiServer *configv1.APIServer) configv1informer.APIServerInformer {
	return &fakeAPIServerInformer{
		lister: &fakeAPIServerLister{apiServer: apiServer},
	}
}

func apiserverCluster(tlsProfile *configv1.TLSSecurityProfile, tlsAdherence configv1.TLSAdherencePolicy) *configv1.APIServer {
	return &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: tlsProfile,
			TLSAdherence:       tlsAdherence,
		},
	}
}

func TestWithClusterTLSProfileFromAPIServer_Webhook(t *testing.T) {
	tests := []struct {
		name           string
		tlsProfile     *configv1.TLSSecurityProfile
		tlsAdherence   configv1.TLSAdherencePolicy
		existingArgs   []string
		expectedArgs   []string
		expectError    bool
		deploymentName string
		containerCount int
	}{
		{
			name: "webhook gets main and metrics TLS flags with Intermediate profile",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
				"--secure-port=10250",
			},
			expectedArgs: []string{
				"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--metrics-tls-min-version=VersionTLS12",
				"--secure-port=10250",
				"--tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--tls-min-version=VersionTLS12",
				"--v=2",
			},
			expectError: false,
		},
		{
			name:           "webhook gets TLS flags with nil profile (defaults to Intermediate)",
			tlsProfile:     nil,
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--metrics-tls-min-version=VersionTLS12",
				"--tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--tls-min-version=VersionTLS12",
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "webhook gets TLS flags with Modern profile",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--metrics-tls-min-version=VersionTLS13",
				"--tls-min-version=VersionTLS13",
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "webhook gets TLS flags with Old profile",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileOldType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,TLS_RSA_WITH_AES_128_GCM_SHA256,TLS_RSA_WITH_AES_256_GCM_SHA384,TLS_RSA_WITH_AES_128_CBC_SHA256,TLS_RSA_WITH_AES_128_CBC_SHA,TLS_RSA_WITH_AES_256_CBC_SHA,TLS_RSA_WITH_3DES_EDE_CBC_SHA",
				"--metrics-tls-min-version=VersionTLS10",
				"--tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256,TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,TLS_RSA_WITH_AES_128_GCM_SHA256,TLS_RSA_WITH_AES_256_GCM_SHA384,TLS_RSA_WITH_AES_128_CBC_SHA256,TLS_RSA_WITH_AES_128_CBC_SHA,TLS_RSA_WITH_AES_256_CBC_SHA,TLS_RSA_WITH_3DES_EDE_CBC_SHA",
				"--tls-min-version=VersionTLS10",
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "webhook gets TLS flags with Custom profile",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileCustomType,
				Custom: &configv1.CustomTLSProfile{
					TLSProfileSpec: configv1.TLSProfileSpec{
						Ciphers: []string{
							"ECDHE-RSA-AES128-GCM-SHA256",
							"TLS_AES_128_GCM_SHA256",
						},
						MinTLSVersion: configv1.VersionTLS12,
					},
				},
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--metrics-tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_AES_128_GCM_SHA256",
				"--metrics-tls-min-version=VersionTLS12",
				"--tls-cipher-suites=TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_AES_128_GCM_SHA256",
				"--tls-min-version=VersionTLS12",
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "webhook TLS flags override existing TLS flags",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
				"--tls-min-version=VersionTLS10",
				"--tls-cipher-suites=OLD_CIPHER",
			},
			expectedArgs: []string{
				"--metrics-tls-min-version=VersionTLS13",
				"--tls-min-version=VersionTLS13",
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "webhook strips stale metrics and main TLS cipher args when effective min TLS is 1.3",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
				"--tls-cipher-suites=STALE_MAIN",
				"--metrics-tls-cipher-suites=STALE_METRICS",
				"--tls-min-version=VersionTLS12",
				"--metrics-tls-min-version=VersionTLS12",
			},
			expectedArgs: []string{
				"--metrics-tls-min-version=VersionTLS13",
				"--tls-min-version=VersionTLS13",
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "multiple containers deployment returns error",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 2,
			existingArgs: []string{
				"--v=2",
			},
			expectError: true,
		},
		{
			name: "zero containers deployment returns error",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 0,
			existingArgs: []string{
				"--v=2",
			},
			expectError: true,
		},
		{
			name: "webhook does not get cluster TLS flags when tlsAdherence is LegacyAdheringComponentsOnly",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
				"--secure-port=10250",
			},
			expectedArgs: []string{
				"--v=2",
				"--secure-port=10250",
			},
			expectError: false,
		},
		{
			name: "webhook does not get cluster TLS flags when tlsAdherence is unset",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyNoOpinion,
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--v=2",
			},
			expectError: false,
		},
		{
			name: "unknown tlsAdherence is treated as strict for TLS args",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicy("UnknownFuturePolicy"),
			deploymentName: certmanagerWebhookDeployment,
			containerCount: 1,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--metrics-tls-min-version=VersionTLS12",
				"--tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--tls-min-version=VersionTLS12",
				"--v=2",
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiServer := apiserverCluster(tt.tlsProfile, tt.tlsAdherence)

			// Create fake informer
			apiServerInformer := newFakeAPIServerInformer(apiServer)

			// Create deployment with specified number of containers
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.deploymentName,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: make([]corev1.Container, tt.containerCount),
						},
					},
				},
			}

			for i := range tt.containerCount {
				deployment.Spec.Template.Spec.Containers[i] = corev1.Container{
					Name: "test-container",
					Args: append([]string{}, tt.existingArgs...),
				}
			}

			// Apply the hook
			hook := WithClusterTLSProfileFromAPIServer(apiServerInformer)
			err := hook(&operatorv1.OperatorSpec{}, deployment)

			if tt.expectError {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)

			// Verify args for the first container (if exists)
			if tt.containerCount > 0 {
				actualArgs := deployment.Spec.Template.Spec.Containers[0].Args
				if diff := cmp.Diff(tt.expectedArgs, actualArgs); diff != "" {
					t.Errorf("Args mismatch (-want +got):\n%s", diff)
				}
			}
		})
	}
}

func TestWithClusterTLSProfileFromAPIServer_Controller(t *testing.T) {
	tests := []struct {
		name           string
		tlsProfile     *configv1.TLSSecurityProfile
		tlsAdherence   configv1.TLSAdherencePolicy
		existingArgs   []string
		expectedArgs   []string
		deploymentName string
	}{
		{
			name: "controller gets metrics TLS flags only",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerControllerDeployment,
			existingArgs: []string{
				"--v=2",
				"--cluster-resource-namespace=$(POD_NAMESPACE)",
			},
			expectedArgs: []string{
				"--cluster-resource-namespace=$(POD_NAMESPACE)",
				"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--metrics-tls-min-version=VersionTLS12",
				"--v=2",
			},
		},
		{
			name: "controller gets Modern profile metrics TLS flags",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerControllerDeployment,
			existingArgs: []string{
				"--v=2",
			},
			expectedArgs: []string{
				"--metrics-tls-min-version=VersionTLS13",
				"--v=2",
			},
		},
		{
			name: "controller does not get cluster TLS flags when tlsAdherence is legacy",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyLegacyAdheringComponentsOnly,
			deploymentName: certmanagerControllerDeployment,
			existingArgs: []string{
				"--v=2",
				"--cluster-resource-namespace=$(POD_NAMESPACE)",
			},
			expectedArgs: []string{
				"--v=2",
				"--cluster-resource-namespace=$(POD_NAMESPACE)",
			},
		},
		{
			name: "controller strips stale metrics TLS cipher args when effective min TLS is 1.3",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileModernType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerControllerDeployment,
			existingArgs: []string{
				"--v=2",
				"--metrics-tls-cipher-suites=STALE_SUITE",
				"--metrics-tls-min-version=VersionTLS12",
			},
			expectedArgs: []string{
				"--metrics-tls-min-version=VersionTLS13",
				"--v=2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiServer := apiserverCluster(tt.tlsProfile, tt.tlsAdherence)

			apiServerInformer := newFakeAPIServerInformer(apiServer)

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.deploymentName,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "cert-manager",
									Args: append([]string{}, tt.existingArgs...),
								},
							},
						},
					},
				},
			}

			hook := WithClusterTLSProfileFromAPIServer(apiServerInformer)
			err := hook(&operatorv1.OperatorSpec{}, deployment)
			require.NoError(t, err)

			actualArgs := deployment.Spec.Template.Spec.Containers[0].Args
			if diff := cmp.Diff(tt.expectedArgs, actualArgs); diff != "" {
				t.Errorf("Args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWithClusterTLSProfileFromAPIServer_CAInjector(t *testing.T) {
	tests := []struct {
		name           string
		tlsProfile     *configv1.TLSSecurityProfile
		tlsAdherence   configv1.TLSAdherencePolicy
		existingArgs   []string
		expectedArgs   []string
		deploymentName string
	}{
		{
			name: "cainjector gets metrics TLS flags only",
			tlsProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
			tlsAdherence:   configv1.TLSAdherencePolicyStrictAllComponents,
			deploymentName: certmanagerCAinjectorDeployment,
			existingArgs: []string{
				"--v=2",
				"--leader-election-namespace=kube-system",
			},
			expectedArgs: []string{
				"--leader-election-namespace=kube-system",
				"--metrics-tls-cipher-suites=TLS_AES_128_GCM_SHA256,TLS_AES_256_GCM_SHA384,TLS_CHACHA20_POLY1305_SHA256,TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256",
				"--metrics-tls-min-version=VersionTLS12",
				"--v=2",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			apiServer := apiserverCluster(tt.tlsProfile, tt.tlsAdherence)

			apiServerInformer := newFakeAPIServerInformer(apiServer)

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name: tt.deploymentName,
				},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name: "cert-manager-cainjector",
									Args: append([]string{}, tt.existingArgs...),
								},
							},
						},
					},
				},
			}

			hook := WithClusterTLSProfileFromAPIServer(apiServerInformer)
			err := hook(&operatorv1.OperatorSpec{}, deployment)
			require.NoError(t, err)

			actualArgs := deployment.Spec.Template.Spec.Containers[0].Args
			if diff := cmp.Diff(tt.expectedArgs, actualArgs); diff != "" {
				t.Errorf("Args mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestWithClusterTLSProfileFromAPIServer_UnknownDeployment(t *testing.T) {
	apiServer := &configv1.APIServer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster",
		},
		Spec: configv1.APIServerSpec{
			TLSSecurityProfile: &configv1.TLSSecurityProfile{
				Type: configv1.TLSProfileIntermediateType,
			},
		},
	}

	apiServerInformer := newFakeAPIServerInformer(apiServer)

	existingArgs := []string{"--v=2", "--some-flag=value"}
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "unknown-deployment",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "unknown",
							Args: append([]string{}, existingArgs...),
						},
					},
				},
			},
		},
	}

	hook := WithClusterTLSProfileFromAPIServer(apiServerInformer)
	err := hook(&operatorv1.OperatorSpec{}, deployment)
	require.NoError(t, err)

	// Args should remain unchanged
	actualArgs := deployment.Spec.Template.Spec.Containers[0].Args
	if diff := cmp.Diff(existingArgs, actualArgs); diff != "" {
		t.Errorf("Args should not be modified for unknown deployment (-want +got):\n%s", diff)
	}
}

func TestWithClusterTLSProfileFromAPIServer_APIServerNotFound(t *testing.T) {
	// Create fake informer without the APIServer resource
	apiServerInformer := newFakeAPIServerInformer(nil)

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: certmanagerWebhookDeployment,
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "webhook",
							Args: []string{"--v=2"},
						},
					},
				},
			},
		},
	}

	hook := WithClusterTLSProfileFromAPIServer(apiServerInformer)
	err := hook(&operatorv1.OperatorSpec{}, deployment)

	// Should return an error when APIServer resource is not found
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to get apiserver.config.openshift.io/cluster")
}
