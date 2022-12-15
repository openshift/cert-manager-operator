package deployment

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
)

func TestUnsupportedConfigOverrides(t *testing.T) {
	deploymentAssetPaths := map[string]string{
		"cert-manager":            "cert-manager-deployment/controller/cert-manager-deployment.yaml",
		"cert-manager-cainjector": "cert-manager-deployment/cainjector/cert-manager-cainjector-deployment.yaml",
		"cert-manager-webhook":    "cert-manager-deployment/webhook/cert-manager-webhook-deployment.yaml",
	}
	deployments := make(map[string]*appsv1.Deployment)
	for deploymentName, assetPath := range deploymentAssetPaths {
		manifestFile, err := assets.Asset(assetPath)
		require.NoError(t, err)
		deployments[deploymentName] = resourceread.ReadDeploymentV1OrDie(manifestFile)
	}

	deploymentDefaultArgs := make(map[string][]string)
	for deploymentName := range deployments {
		deploymentDefaultArgs[deploymentName] = deployments[deploymentName].Spec.Template.Spec.Containers[0].Args
	}

	testArgsToAppend := []string{
		"--test-arg", "--featureX=enable",
	}
	testArgsToOverrideReplace := []string{
		"--v=5", "--featureY=disable",
	}

	type TestData struct {
		deploymentName string
		overrides      *v1alpha1.UnsupportedConfigOverrides
		wantArgs       []string
	}
	tests := map[string]TestData{
		// unsupported config overrides as nil
		"nil config overrides should not touch the controller deployment": {
			deploymentName: "cert-manager",
			overrides:      nil,
			wantArgs:       deploymentDefaultArgs["cert-manager"],
		},
		"nil config overrides should not touch the cainjector deployment": {
			deploymentName: "cert-manager-cainjector",
			overrides:      nil,
			wantArgs:       deploymentDefaultArgs["cert-manager-cainjector"],
		},
		"nil config overrides should not touch the webhook deployment": {
			deploymentName: "cert-manager-webhook",
			overrides:      nil,
			wantArgs:       deploymentDefaultArgs["cert-manager-webhook"],
		},

		// unsupported config overrides as empty
		"Empty config overrides should not touch the controller deployment": {
			deploymentName: "cert-manager",
			overrides:      &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:       deploymentDefaultArgs["cert-manager"],
		},
		"Empty config overrides should not touch the cainjector deployment": {
			deploymentName: "cert-manager-cainjector",
			overrides:      &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:       deploymentDefaultArgs["cert-manager-cainjector"],
		},
		"Empty config overrides should not touch the webhook deployment": {
			deploymentName: "cert-manager-webhook",
			overrides:      &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:       deploymentDefaultArgs["cert-manager-webhook"],
		},

		// unsupported config overrides for webhook, cainjector should not
		// modify controller deployment
		"Other config overrides should not touch the controller deployment": {
			deploymentName: "cert-manager",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: testArgsToAppend,
				},
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: testArgsToAppend,
				},
			},
			wantArgs: deploymentDefaultArgs["cert-manager"],
		},

		// unsupported config overrides as a mechanism of appending new args
		"Controller overrides should append newer overriden values": {
			deploymentName: "cert-manager",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Controller: v1alpha1.UnsupportedConfigOverridesForCertManagerController{
					Args: testArgsToAppend,
				},
			},
			wantArgs: append(deploymentDefaultArgs["cert-manager"], testArgsToAppend...),
		},
		"CAInjector overrides should append newer overriden values": {
			deploymentName: "cert-manager-cainjector",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: testArgsToAppend,
				},
			},
			wantArgs: append(deploymentDefaultArgs["cert-manager-cainjector"], testArgsToAppend...),
		},
		"Webhook overrides should append newer overriden values": {
			deploymentName: "cert-manager-webhook",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: testArgsToAppend,
				},
			},
			wantArgs: append(deploymentDefaultArgs["cert-manager-webhook"], testArgsToAppend...),
		},

		// unsupported config overrides as a mechanism of replacing existing values
		// of already present args
		"Controller overrides existing values for --v": {
			deploymentName: "cert-manager",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Controller: v1alpha1.UnsupportedConfigOverridesForCertManagerController{
					Args: testArgsToOverrideReplace,
				},
			},
			wantArgs: append(
				removeFromSlice(deploymentDefaultArgs["cert-manager"], "--v="),
				testArgsToOverrideReplace...,
			),
		},
		"CAInjector overrides existing values for --v": {
			deploymentName: "cert-manager-cainjector",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: testArgsToOverrideReplace,
				},
			},
			wantArgs: append(
				removeFromSlice(deploymentDefaultArgs["cert-manager-cainjector"], "--v="),
				testArgsToOverrideReplace...,
			),
		},
		"Webhook overrides existing values for --v": {
			deploymentName: "cert-manager-webhook",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: testArgsToOverrideReplace,
				},
			},
			wantArgs: append(
				removeFromSlice(deploymentDefaultArgs["cert-manager-webhook"], "--v="),
				testArgsToOverrideReplace...,
			),
		},
	}

	for tcName, tcData := range tests {
		tcName := tcName
		tcData := tcData
		t.Run(tcName, func(t *testing.T) {
			t.Parallel()
			newDeployment := UnsupportedConfigOverrides(deployments[tcData.deploymentName].DeepCopy(), tcData.overrides)
			require.ElementsMatch(t, tcData.wantArgs, newDeployment.Spec.Template.Spec.Containers[0].Args)
		})
	}
}

func TestParseArgMap(t *testing.T) {
	testArgs := []string{
		"", // should be ignored at the time of parse
		"--", "--foo", "--v=1", "--test=v1=v2", "--gates=Feature1=True",
		"--log-level=Debug=false,Info=false,Warning=True,Error=true",
		"--extra-flags='--v=2 --gates=Feature2=True'",
	}
	wantMap := map[string]string{
		"--":            "",
		"--foo":         "",
		"--v":           "1",
		"--test":        "v1=v2",
		"--gates":       "Feature1=True",
		"--log-level":   "Debug=false,Info=false,Warning=True,Error=true",
		"--extra-flags": "'--v=2 --gates=Feature2=True'",
	}

	argMap := make(map[string]string)
	parseArgMap(argMap, testArgs)
	if !reflect.DeepEqual(argMap, wantMap) {
		t.Fatalf("unexpected update to arg map, diff = %v", cmp.Diff(wantMap, argMap))
	}
}

// removeFromSlice constructs a new slice by removing string(s) with prefix
func removeFromSlice(args []string, removalPrefix string) []string {
	targetArgs := []string{}
	for _, arg := range args {
		if !strings.HasPrefix(arg, removalPrefix) {
			targetArgs = append(targetArgs, arg)
		}
	}
	return targetArgs
}
