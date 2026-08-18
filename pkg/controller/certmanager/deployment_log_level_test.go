package certmanager

import (
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newTestDeploymentWithArgs(args []string) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cert-manager",
		},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "cert-manager-controller",
							Args: args,
						},
					},
				},
			},
		},
	}
}

func TestWithLogLevel(t *testing.T) {
	tests := []struct {
		name         string
		logLevel     operatorv1.LogLevel
		existingArgs []string
		wantArgs     []string
		wantChanged  bool
	}{
		{
			name:         "Normal log level sets --v=2",
			logLevel:     operatorv1.Normal,
			existingArgs: []string{"--cluster-resource-namespace=cert-manager"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=2"},
			wantChanged:  true,
		},
		{
			name:         "Debug log level sets --v=4",
			logLevel:     operatorv1.Debug,
			existingArgs: []string{"--cluster-resource-namespace=cert-manager"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=4"},
			wantChanged:  true,
		},
		{
			name:         "Trace log level sets --v=6",
			logLevel:     operatorv1.Trace,
			existingArgs: []string{"--cluster-resource-namespace=cert-manager"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=6"},
			wantChanged:  true,
		},
		{
			name:         "TraceAll log level sets --v=8",
			logLevel:     operatorv1.TraceAll,
			existingArgs: []string{"--cluster-resource-namespace=cert-manager"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=8"},
			wantChanged:  true,
		},
		{
			name:         "empty log level does not modify args",
			logLevel:     "",
			existingArgs: []string{"--cluster-resource-namespace=cert-manager", "--v=2"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=2"},
			wantChanged:  false,
		},
		{
			name:         "unknown log level does not modify args",
			logLevel:     "UnknownLevel",
			existingArgs: []string{"--cluster-resource-namespace=cert-manager", "--v=2"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=2"},
			wantChanged:  false,
		},
		{
			name:         "log level overrides existing --v arg",
			logLevel:     operatorv1.Debug,
			existingArgs: []string{"--cluster-resource-namespace=cert-manager", "--v=2"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--v=4"},
			wantChanged:  true,
		},
		{
			name:         "log level merges with multiple existing args",
			logLevel:     operatorv1.Trace,
			existingArgs: []string{"--leader-election-namespace=kube-system", "--v=2", "--cluster-resource-namespace=cert-manager"},
			wantArgs:     []string{"--cluster-resource-namespace=cert-manager", "--leader-election-namespace=kube-system", "--v=6"},
			wantChanged:  true,
		},
		{
			name:         "log level works with empty existing args",
			logLevel:     operatorv1.Normal,
			existingArgs: nil,
			wantArgs:     []string{"--v=2"},
			wantChanged:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			deployment := newTestDeploymentWithArgs(tc.existingArgs)
			operatorSpec := &operatorv1.OperatorSpec{
				LogLevel: tc.logLevel,
			}

			err := withLogLevel(operatorSpec, deployment)
			require.NoError(t, err)
			require.Equal(t, tc.wantArgs, deployment.Spec.Template.Spec.Containers[0].Args)
		})
	}
}
