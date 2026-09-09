package certmanager

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestWithOperandMetricsTLS(t *testing.T) {
	controllerWantArgs := []string{
		"--metrics-dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
		"--metrics-dynamic-serving-ca-secret-name=cert-manager-metrics-ca",
		"--metrics-dynamic-serving-dns-names=cert-manager,cert-manager.$(POD_NAMESPACE),cert-manager.$(POD_NAMESPACE).svc",
	}
	webhookWantArgs := []string{
		"--metrics-dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
		"--metrics-dynamic-serving-ca-secret-name=cert-manager-metrics-ca",
		"--metrics-dynamic-serving-dns-names=cert-manager-webhook,cert-manager-webhook.$(POD_NAMESPACE),cert-manager-webhook.$(POD_NAMESPACE).svc",
	}
	cainjectorWantArgs := []string{
		"--metrics-dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
		"--metrics-dynamic-serving-ca-secret-name=cert-manager-metrics-ca",
		"--metrics-dynamic-serving-dns-names=cert-manager-cainjector,cert-manager-cainjector.$(POD_NAMESPACE),cert-manager-cainjector.$(POD_NAMESPACE).svc",
	}

	tests := []struct {
		name            string
		deploymentName  string
		annotations     map[string]string
		args            []string
		noContainers    bool
		extraContainer  bool
		applyTwice      bool
		wantErrContains string
		wantArgs        []string
		wantScheme      string
		wantPort        string
		wantV           string
	}{
		{
			name:            "error: no containers",
			deploymentName:  certmanagerControllerDeployment,
			noContainers:    true,
			wantErrContains: "expected 1 container for metrics TLS hook, got 0",
		},
		{
			name:            "error: more than one container",
			deploymentName:  certmanagerControllerDeployment,
			args:            []string{"--v=2"},
			extraContainer:  true,
			wantErrContains: "expected 1 container for metrics TLS hook, got 2",
		},
		{
			name:           "no-op: unknown deployment name",
			deploymentName: "not-an-operand",
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			wantPort:       "9402",
			wantV:          "2",
		},
		{
			name:           "nil annotations → scheme set, map created",
			deploymentName: certmanagerControllerDeployment,
			annotations:    nil,
			args:           []string{"--v=2"},
			wantArgs:       controllerWantArgs,
			wantScheme:     "https",
			wantV:          "2",
		},
		{
			name:           "preserve prometheus.io/port",
			deploymentName: certmanagerControllerDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			wantArgs:       controllerWantArgs,
			wantScheme:     "https",
			wantPort:       "9402",
			wantV:          "2",
		},
		{
			name:           "preserve --v=2",
			deploymentName: certmanagerWebhookDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			wantArgs:       webhookWantArgs,
			wantScheme:     "https",
			wantPort:       "9402",
			wantV:          "2",
		},
		{
			name:           "idempotent second apply",
			deploymentName: certmanagerCAinjectorDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			applyTwice:     true,
			wantArgs:       cainjectorWantArgs,
			wantScheme:     "https",
			wantPort:       "9402",
			wantV:          "2",
		},
		{
			name:           "override stale --metrics-dynamic-serving-dns-names",
			deploymentName: certmanagerControllerDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args: []string{
				"--v=2",
				"--metrics-dynamic-serving-dns-names=stale.example",
			},
			wantArgs:   controllerWantArgs,
			wantScheme: "https",
			wantPort:   "9402",
			wantV:      "2",
		},
		{
			name:           "controller",
			deploymentName: certmanagerControllerDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			wantArgs:       controllerWantArgs,
			wantScheme:     "https",
			wantPort:       "9402",
			wantV:          "2",
		},
		{
			name:           "webhook",
			deploymentName: certmanagerWebhookDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			wantArgs:       webhookWantArgs,
			wantScheme:     "https",
			wantPort:       "9402",
			wantV:          "2",
		},
		{
			name:           "cainjector",
			deploymentName: certmanagerCAinjectorDeployment,
			annotations:    map[string]string{"prometheus.io/port": "9402"},
			args:           []string{"--v=2"},
			wantArgs:       cainjectorWantArgs,
			wantScheme:     "https",
			wantPort:       "9402",
			wantV:          "2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dep := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: tt.deploymentName, Namespace: "cert-manager"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: tt.annotations,
						},
					},
				},
			}
			if !tt.noContainers {
				dep.Spec.Template.Spec.Containers = []corev1.Container{{
					Name: tt.deploymentName,
					Args: append([]string{}, tt.args...),
				}}
				if tt.extraContainer {
					dep.Spec.Template.Spec.Containers = append(dep.Spec.Template.Spec.Containers, corev1.Container{
						Name: "sidecar",
					})
				}
			}

			err := withOperandMetricsTLS(nil, dep)
			if tt.wantErrContains != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErrContains)
				return
			}
			require.NoError(t, err)

			if tt.applyTwice {
				afterFirst := append([]string{}, dep.Spec.Template.Spec.Containers[0].Args...)
				require.NoError(t, withOperandMetricsTLS(nil, dep))
				require.Equal(t, afterFirst, dep.Spec.Template.Spec.Containers[0].Args)
			}

			require.NotNil(t, dep.Spec.Template.Annotations)
			if tt.wantScheme != "" {
				require.Equal(t, tt.wantScheme, dep.Spec.Template.Annotations["prometheus.io/scheme"])
			} else {
				_, hasScheme := dep.Spec.Template.Annotations["prometheus.io/scheme"]
				require.False(t, hasScheme, "unknown deployment must not set prometheus.io/scheme")
			}
			if tt.wantPort != "" {
				require.Equal(t, tt.wantPort, dep.Spec.Template.Annotations["prometheus.io/port"])
			}

			gotArgs := argsByKey(dep.Spec.Template.Spec.Containers[0].Args)
			if tt.wantV != "" {
				require.Equal(t, tt.wantV, gotArgs["--v"])
			}
			for _, want := range tt.wantArgs {
				key, val, ok := strings.Cut(want, "=")
				require.True(t, ok, "want arg %q", want)
				require.Equal(t, val, gotArgs[key], "arg %s", key)
			}
			if len(tt.wantArgs) == 0 {
				for key := range gotArgs {
					require.False(t, strings.HasPrefix(key, "--metrics-dynamic-serving-"), "unexpected arg %s", key)
				}
			}
		})
	}
}

func argsByKey(args []string) map[string]string {
	m := map[string]string{}
	common.ParseArgMap(m, args)
	return m
}
