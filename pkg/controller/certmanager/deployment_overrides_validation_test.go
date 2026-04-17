package certmanager

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	k8sresource "k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

func TestValidateResources(t *testing.T) {
	tests := []struct {
		name                   string
		resources              v1alpha1.CertManagerResourceRequirements
		resourceNamesSupported []string
		errorExpected          bool
	}{
		{
			name: "validate cpu resource name in resources limits",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU)},
			errorExpected:          false,
		},
		{
			name: "validate memory resource name in resources limits",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu and memory resource names in resources limits",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu resource name in resources requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU: k8sresource.MustParse("10m"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU)},
			errorExpected:          false,
		},
		{
			name: "validate memory resource name in resources requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu and memory resource names in resources requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "validate cpu and memory resource names in resources limits and requests",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceCPU:    k8sresource.MustParse("10m"),
					corev1.ResourceMemory: k8sresource.MustParse("32Mi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          false,
		},
		{
			name: "unsupported resource name in resources limits and requests should return error",
			resources: v1alpha1.CertManagerResourceRequirements{
				Limits: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("10Gi"),
				},
				Requests: corev1.ResourceList{
					corev1.ResourceEphemeralStorage: k8sresource.MustParse("10Gi"),
				},
			},
			resourceNamesSupported: []string{string(corev1.ResourceCPU), string(corev1.ResourceMemory)},
			errorExpected:          true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateResources(tc.resources, tc.resourceNamesSupported)
			if tc.errorExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestValidateScheduling(t *testing.T) {
	tests := []struct {
		name          string
		scheduling    v1alpha1.CertManagerScheduling
		errorExpected bool
	}{
		{
			name: "valid node selector should be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"nodeLabel1": "value1",
					"nodeLabel2": "value2",
				},
			},
			errorExpected: false,
		},
		{
			name: "invalid node selector should not be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				NodeSelector: map[string]string{
					"/nodeLabel1":  "value1",
					"node/Label/2": "value2",
					"":             "value3",
				},
			},
			errorExpected: true,
		},
		{
			name: "valid tolerations should be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				Tolerations: []corev1.Toleration{
					{
						Key:      "tolerationKey1",
						Operator: "Exists",
						Effect:   "NoSchedule",
					},
					{
						Key:      "tolerationKey2",
						Operator: "Equal",
						Value:    "value2",
						Effect:   "NoSchedule",
					},
					{
						Key:      "tolerationKey3",
						Operator: "Equal",
						Effect:   "NoSchedule",
					},
				},
			},
			errorExpected: false,
		},
		{
			name: "invalid tolerations should not be accepted",
			scheduling: v1alpha1.CertManagerScheduling{
				Tolerations: []corev1.Toleration{
					{
						Key:      "tolerationKey1",
						Operator: "Exists",
						Value:    "value1",
						Effect:   "NoSchedule",
					},
					{
						Key:      "",
						Operator: "Equal",
						Effect:   "NoSchedule",
					},
				},
			},
			errorExpected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateScheduling(tc.scheduling, field.NewPath("overridesScheduling"))
			if tc.errorExpected {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestWithContainerArgsValidateHook(t *testing.T) {
	tests := []struct {
		name            string
		certManagerObj  v1alpha1.CertManager
		deploymentName  string
		wantErrMsg      string
		omitCertManager bool // do not create a CertManager; lister Get("cluster") fails
	}{
		{
			name:            "lister returns error when certmanager cluster is absent",
			deploymentName:  certmanagerControllerDeployment,
			omitCertManager: true,
		},
		{
			name: "controller accepts certificate-request-minimum-backoff-duration",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--certificate-request-minimum-backoff-duration=1m"},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "controller accepts acme-http01-solver-nameservers",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--acme-http01-solver-nameservers=8.8.8.8:53,1.1.1.1:53"},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "controller accepts dns01-recursive-nameservers and dns01-recursive-nameservers-only",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{
							"--dns01-recursive-nameservers=8.8.8.8:53",
							"--dns01-recursive-nameservers-only",
						},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "controller accepts solver resource limit and request flags",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{
							"--acme-http01-solver-resource-limits-cpu=200m",
							"--acme-http01-solver-resource-limits-memory=128Mi",
							"--acme-http01-solver-resource-request-cpu=50m",
							"--acme-http01-solver-resource-request-memory=64Mi",
						},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "controller accepts v metrics-listen-address issuer-ambient-credentials enable-certificate-owner-ref",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{
							"--v=2",
							"-V=1",
							"--metrics-listen-address=0.0.0.0:9402",
							"--issuer-ambient-credentials",
							"--enable-certificate-owner-ref",
						},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "controller rejects unsupported override arg",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--totally-unknown-flag=value"},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
			wantErrMsg:     `validation failed due to unsupported arg "--totally-unknown-flag"="value"`,
		},
		{
			name: "controller validates only controllerConfig webhook override args ignored",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--v=2"},
					},
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--totally-unknown-flag=would-fail-if-validated-here"},
					},
				},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "nil controller config skips validation",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       v1alpha1.CertManagerSpec{},
			},
			deploymentName: certmanagerControllerDeployment,
		},
		{
			name: "webhook accepts v override",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--v=3"},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
		},
		{
			name: "webhook accepts V override",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"-V=1"},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
		},
		{
			name: "webhook rejects controller-only arg",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--metrics-listen-address=0.0.0.0:9402"},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
			wantErrMsg:     `validation failed due to unsupported arg "--metrics-listen-address"="0.0.0.0:9402"`,
		},
		{
			name: "webhook rejects certificate-request-minimum-backoff-duration",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--certificate-request-minimum-backoff-duration=1m"},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
			wantErrMsg:     `validation failed due to unsupported arg "--certificate-request-minimum-backoff-duration"="1m"`,
		},
		{
			name: "nil webhook config skips validation",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       v1alpha1.CertManagerSpec{},
			},
			deploymentName: certmanagerWebhookDeployment,
		},
		{
			name: "webhook validates only webhookConfig controller override args ignored",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--totally-unknown-controller-flag=x"},
					},
					WebhookConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--v=2"},
					},
				},
			},
			deploymentName: certmanagerWebhookDeployment,
		},
		{
			name: "cainjector accepts v override",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--v=4"},
					},
				},
			},
			deploymentName: certmanagerCAinjectorDeployment,
		},
		{
			name: "cainjector rejects controller-only arg",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--dns01-recursive-nameservers=8.8.8.8:53"},
					},
				},
			},
			deploymentName: certmanagerCAinjectorDeployment,
			wantErrMsg:     `validation failed due to unsupported arg "--dns01-recursive-nameservers"="8.8.8.8:53"`,
		},
		{
			name: "nil cainjector config skips validation",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       v1alpha1.CertManagerSpec{},
			},
			deploymentName: certmanagerCAinjectorDeployment,
		},
		{
			name: "cainjector validates only cainjectorConfig controller override args ignored",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--totally-unknown-controller-flag=x"},
					},
					CAInjectorConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--v=2"},
					},
				},
			},
			deploymentName: certmanagerCAinjectorDeployment,
		},
		{
			name: "unsupported deployment name returns error",
			certManagerObj: v1alpha1.CertManager{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: v1alpha1.CertManagerSpec{
					ControllerConfig: &v1alpha1.DeploymentConfig{
						OverrideArgs: []string{"--v=2"},
					},
				},
			},
			deploymentName: "unknown-cert-manager-deployment",
			wantErrMsg:     `unsupported deployment name "unknown-cert-manager-deployment" provided`,
		},
	}

	ctx := t.Context()
	fakeClient, certManagerInformers, certManagerChan := setupSyncedFakeCertManagerInformer(t, ctx)

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.omitCertManager {
				hook := withContainerArgsValidateHook(certManagerInformers, tc.deploymentName)
				hookErr := hook(nil, &appsv1.Deployment{})
				require.Error(t, hookErr)
				assert.Contains(t, hookErr.Error(), `failed to get certmanager "cluster"`)
				assert.True(t, apierrors.IsNotFound(hookErr), "expected NotFound from lister when CertManager cluster is absent")
				return
			}

			withFakeCertManagerForTest(t, ctx, fakeClient, certManagerChan, &tc.certManagerObj)

			hook := withContainerArgsValidateHook(certManagerInformers, tc.deploymentName)
			hookErr := hook(nil, &appsv1.Deployment{})

			if tc.wantErrMsg != "" {
				require.EqualError(t, hookErr, tc.wantErrMsg)
			} else {
				require.NoError(t, hookErr)
			}
		})
	}
}
