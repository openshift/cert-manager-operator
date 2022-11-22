package deployment

import (
	"testing"

	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/stretchr/testify/require"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func TestUnsupportedConfigOverrides(t *testing.T) {
	deploymentFile, err := assets.Asset("cert-manager-deployment/controller/cert-manager-deployment.yaml")
	require.NoError(t, err)
	testDeployment := resourceread.ReadDeploymentV1OrDie(deploymentFile)
	defaultArgs := testDeployment.Spec.Template.Spec.Containers[0].Args

	type TestData struct {
		overrides *v1alpha1.UnsupportedConfigOverrides
		wantArgs  []string
	}
	tests := map[string]TestData{
		"nil config overrides should not touch the deployment": {
			overrides: nil,
			wantArgs:  defaultArgs,
		},
		"Empty config overrides should not touch the deployment": {
			overrides: &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:  defaultArgs,
		},
		"Other config overrides should not touch the controller": {
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: []string{"test"},
				},
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: []string{"test"},
				},
			},
			wantArgs: defaultArgs,
		},
		"Controller overrides should work": {
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Controller: v1alpha1.UnsupportedConfigOverridesForCertManagerController{
					Args: []string{"test"},
				},
			},
			wantArgs: []string{"test"},
		},
	}
	for tcName, tcData := range tests {
		tcName := tcName
		tcData := tcData
		t.Run(tcName, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tcData.wantArgs, UnsupportedConfigOverrides(testDeployment.DeepCopy(), tcData.overrides).Spec.Template.Spec.Containers[0].Args)
		})
	}
}
