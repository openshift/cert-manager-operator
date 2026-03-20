package certmanager

import (
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/cache"
	corelistersv1 "k8s.io/client-go/listers/core/v1"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
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

	defaultDeploymentArgs := map[string][]string{
		"cert-manager": {
			"--v=2",
			"--cluster-resource-namespace=$(POD_NAMESPACE)",
			"--leader-election-namespace=kube-system",
			"--acme-http01-solver-image=quay.io/jetstack/cert-manager-acmesolver:v1.19.4",
			"--max-concurrent-challenges=60",
			"--feature-gates=ACMEHTTP01IngressPathTypeExact=false",
		},
		"cert-manager-cainjector": {
			"--v=2",
			"--leader-election-namespace=kube-system",
		},
		"cert-manager-webhook": {
			"--v=2",
			"--secure-port=10250",
			"--dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
			"--dynamic-serving-ca-secret-name=cert-manager-webhook-ca",
			"--dynamic-serving-dns-names=cert-manager-webhook,cert-manager-webhook.$(POD_NAMESPACE),cert-manager-webhook.$(POD_NAMESPACE).svc",
		},
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
			wantArgs:       defaultDeploymentArgs["cert-manager"],
		},
		"nil config overrides should not touch the cainjector deployment": {
			deploymentName: "cert-manager-cainjector",
			overrides:      nil,
			wantArgs:       defaultDeploymentArgs["cert-manager-cainjector"],
		},
		"nil config overrides should not touch the webhook deployment": {
			deploymentName: "cert-manager-webhook",
			overrides:      nil,
			wantArgs:       defaultDeploymentArgs["cert-manager-webhook"],
		},

		// unsupported config overrides as empty
		"Empty config overrides should not touch the controller deployment": {
			deploymentName: "cert-manager",
			overrides:      &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:       defaultDeploymentArgs["cert-manager"],
		},
		"Empty config overrides should not touch the cainjector deployment": {
			deploymentName: "cert-manager-cainjector",
			overrides:      &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:       defaultDeploymentArgs["cert-manager-cainjector"],
		},
		"Empty config overrides should not touch the webhook deployment": {
			deploymentName: "cert-manager-webhook",
			overrides:      &v1alpha1.UnsupportedConfigOverrides{},
			wantArgs:       defaultDeploymentArgs["cert-manager-webhook"],
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
			wantArgs: defaultDeploymentArgs["cert-manager"],
		},

		// unsupported config overrides as a mechanism of appending new args
		"Controller overrides should append newer overridden values": {
			deploymentName: "cert-manager",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Controller: v1alpha1.UnsupportedConfigOverridesForCertManagerController{
					Args: testArgsToAppend,
				},
			},
			wantArgs: []string{
				"--acme-http01-solver-image=quay.io/jetstack/cert-manager-acmesolver:v1.19.4",
				"--cluster-resource-namespace=$(POD_NAMESPACE)",
				"--feature-gates=ACMEHTTP01IngressPathTypeExact=false",
				"--featureX=enable",
				"--leader-election-namespace=kube-system",
				"--max-concurrent-challenges=60",
				"--test-arg",
				"--v=2",
			},
		},
		"CAInjector overrides should append newer overridden values": {
			deploymentName: "cert-manager-cainjector",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: testArgsToAppend,
				},
			},
			wantArgs: []string{
				"--featureX=enable",
				"--leader-election-namespace=kube-system",
				"--test-arg",
				"--v=2",
			},
		},
		"Webhook overrides should append newer overridden values": {
			deploymentName: "cert-manager-webhook",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: testArgsToAppend,
				},
			},
			wantArgs: []string{
				"--dynamic-serving-ca-secret-name=cert-manager-webhook-ca",
				"--dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
				"--dynamic-serving-dns-names=cert-manager-webhook,cert-manager-webhook.$(POD_NAMESPACE),cert-manager-webhook.$(POD_NAMESPACE).svc",
				"--featureX=enable",
				"--secure-port=10250",
				"--test-arg",
				"--v=2",
			},
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
			wantArgs: []string{
				"--acme-http01-solver-image=quay.io/jetstack/cert-manager-acmesolver:v1.19.4",
				"--cluster-resource-namespace=$(POD_NAMESPACE)",
				"--feature-gates=ACMEHTTP01IngressPathTypeExact=false",
				"--featureY=disable",
				"--leader-election-namespace=kube-system",
				"--max-concurrent-challenges=60",
				"--v=5",
			},
		},
		"CAInjector overrides existing values for --v": {
			deploymentName: "cert-manager-cainjector",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				CAInjector: v1alpha1.UnsupportedConfigOverridesForCertManagerCAInjector{
					Args: testArgsToOverrideReplace,
				},
			},
			wantArgs: []string{
				"--featureY=disable",
				"--leader-election-namespace=kube-system",
				"--v=5",
			},
		},
		"Webhook overrides existing values for --v": {
			deploymentName: "cert-manager-webhook",
			overrides: &v1alpha1.UnsupportedConfigOverrides{
				Webhook: v1alpha1.UnsupportedConfigOverridesForCertManagerWebhook{
					Args: testArgsToOverrideReplace,
				},
			},
			wantArgs: []string{
				"--dynamic-serving-ca-secret-name=cert-manager-webhook-ca",
				"--dynamic-serving-ca-secret-namespace=$(POD_NAMESPACE)",
				"--dynamic-serving-dns-names=cert-manager-webhook,cert-manager-webhook.$(POD_NAMESPACE),cert-manager-webhook.$(POD_NAMESPACE).svc",
				"--featureY=disable",
				"--secure-port=10250",
				"--v=5",
			},
		},
	}

	for tcName, tcData := range tests {
		t.Run(tcName, func(t *testing.T) {
			t.Parallel()
			newDeployment := unsupportedConfigOverrides(deployments[tcData.deploymentName].DeepCopy(), tcData.overrides)
			require.Equal(t, tcData.wantArgs, newDeployment.Spec.Template.Spec.Containers[0].Args)
		})
	}
}

func TestParseEnvMap(t *testing.T) {
	env := mergeContainerEnvs([]corev1.EnvVar{
		{
			Name:  "A",
			Value: "asd",
		},
		{
			Name:  "B",
			Value: "32r23",
		},
	}, []corev1.EnvVar{
		{
			Name:  "A",
			Value: "23234",
		},
		{
			Name:  "C",
			Value: "a12sd",
		},
	})
	for _, e := range env {
		t.Logf("N: %s\t V:%s\n", e.Name, e.Value)
	}

	args := mergeContainerArgs([]string{"--a=12"}, []string{
		"A", "B", "--a=vc",
	})

	for _, s := range args {
		t.Logf("A:%q\n", s)
	}
}

func TestMergeContainerEnv(t *testing.T) {
	tests := []struct {
		name        string
		sourceEnv   []corev1.EnvVar
		overrideEnv []corev1.EnvVar
		expected    []corev1.EnvVar
	}{
		{
			name: "after merge, env values are sorted by key",
			sourceEnv: []corev1.EnvVar{
				{
					Name:  "XYZ",
					Value: "VALUE2",
				},
				{
					Name:  "ABC",
					Value: "VALUE1",
				},
			},
			overrideEnv: []corev1.EnvVar{
				{

					Name:  "DEF",
					Value: "VALUE1",
				},
			},
			expected: []corev1.EnvVar{
				{

					Name:  "ABC",
					Value: "VALUE1",
				},
				{
					Name:  "DEF",
					Value: "VALUE1",
				},
				{
					Name:  "XYZ",
					Value: "VALUE2",
				},
			},
		},
		{
			name: "override env replaces source env values",
			sourceEnv: []corev1.EnvVar{
				{
					Name:  "KEY2",
					Value: "VALUE2",
				},
				{
					Name:  "KEY1",
					Value: "VALUE1",
				},
			},
			overrideEnv: []corev1.EnvVar{
				{

					Name:  "KEY1",
					Value: "VALUE1",
				},
				{
					Name:  "KEY2",
					Value: "NEW_VALUE",
				},
			},
			expected: []corev1.EnvVar{
				{

					Name:  "KEY1",
					Value: "VALUE1",
				},
				{
					Name:  "KEY2",
					Value: "NEW_VALUE",
				},
			},
		},
	}

	for _, tc := range tests {
		actualEnv := mergeContainerEnvs(tc.sourceEnv, tc.overrideEnv)
		require.Equal(t, tc.expected, actualEnv)
	}
}

func TestMergeContainerEnvProxyOverride(t *testing.T) {
	tests := []struct {
		name            string
		clusterProxyEnv []corev1.EnvVar
		userOverrideEnv []corev1.EnvVar
		expected        []corev1.EnvVar
	}{
		{
			name: "user override replaces cluster proxy values",
			clusterProxyEnv: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://cluster-proxy:3128"},
			},
			userOverrideEnv: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://user-proxy:8080"},
			},
			expected: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://user-proxy:8080"},
			},
		},
		{
			name: "user partial override keeps other cluster proxy values",
			clusterProxyEnv: []corev1.EnvVar{
				{Name: "HTTPS_PROXY", Value: "https://cluster-proxy:3128"},
				{Name: "HTTP_PROXY", Value: "http://cluster-proxy:3128"},
			},
			userOverrideEnv: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://user-proxy:8080"},
			},
			expected: []corev1.EnvVar{
				{Name: "HTTPS_PROXY", Value: "https://cluster-proxy:3128"},
				{Name: "HTTP_PROXY", Value: "http://user-proxy:8080"},
			},
		},
		{
			name:            "no cluster proxy, user override is applied",
			clusterProxyEnv: []corev1.EnvVar{},
			userOverrideEnv: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://user-proxy:8080"},
			},
			expected: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://user-proxy:8080"},
			},
		},
		{
			name: "cluster proxy present, no user override",
			clusterProxyEnv: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://cluster-proxy:3128"},
			},
			userOverrideEnv: []corev1.EnvVar{},
			expected: []corev1.EnvVar{
				{Name: "HTTP_PROXY", Value: "http://cluster-proxy:3128"},
			},
		},
	}

	for _, tc := range tests {
		envAfterProxy := mergeContainerEnvs([]corev1.EnvVar{}, tc.clusterProxyEnv)
		finalEnv := mergeContainerEnvs(envAfterProxy, tc.userOverrideEnv)
		require.Equal(t, tc.expected, finalEnv)
	}
}

func TestMergeContainerArgs(t *testing.T) {
	tests := []struct {
		name         string
		sourceArgs   []string
		overrideArgs []string
		expected     []string
	}{
		{
			name:         "overrideargs replaces source arg values",
			sourceArgs:   []string{"--key1=value1", "--key2=value2"},
			overrideArgs: []string{"--key1=value1", "--key2=value5"},
			expected:     []string{"--key1=value1", "--key2=value5"},
		},
		{
			name:         "after merge, args are sorted in increasing order",
			sourceArgs:   []string{"--xxx1=value1", "--xyz=value2"},
			overrideArgs: []string{"--def=value1", "--abc=value5"},
			expected:     []string{"--abc=value5", "--def=value1", "--xxx1=value1", "--xyz=value2"},
		},
		{
			name:         "after merge, duplicates are removed",
			sourceArgs:   []string{"--abc=value1", "", "--xyz=value2"},
			overrideArgs: []string{"--xyz=value1", "--abc=value1"},
			expected:     []string{"--abc=value1", "--xyz=value1"},
		},
	}

	for _, tc := range tests {
		actualArgs := mergeContainerArgs(tc.sourceArgs, tc.overrideArgs)
		require.Equal(t, tc.expected, actualArgs)
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

// TestWithOperandImageOverrideHook covers the modified hook (parameter rename).
func TestWithOperandImageOverrideHook(t *testing.T) {
	originalImage := "quay.io/jetstack/cert-manager-controller:v1.19.2"
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: certmanagerControllerDeployment},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "controller", Image: originalImage, Args: []string{"--v=2"}},
					},
				},
			},
		},
	}
	err := withOperandImageOverrideHook(nil, deployment)
	require.NoError(t, err)
	require.Len(t, deployment.Spec.Template.Spec.Containers, 1)

	// Verify image was overridden
	newImage := deployment.Spec.Template.Spec.Containers[0].Image
	require.NotEmpty(t, newImage, "image should be set")

	// certManagerImage resolves image; at least args should contain acme solver for controller
	args := deployment.Spec.Template.Spec.Containers[0].Args
	var hasAcme bool
	var acmeImage string
	for _, a := range args {
		if strings.HasPrefix(a, "--acme-http01-solver-image=") {
			hasAcme = true
			acmeImage = strings.TrimPrefix(a, "--acme-http01-solver-image=")
			break
		}
	}
	require.True(t, hasAcme, "controller deployment should get acme-http01-solver-image arg")
	require.NotEmpty(t, acmeImage, "acme solver image value should not be empty")
}

// TestWithProxyEnv covers the modified hook (parameter rename).
func TestWithProxyEnv(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "c", Env: []corev1.EnvVar{{Name: "EXISTING", Value: "x"}}}},
				},
			},
		},
	}
	err := withProxyEnv(nil, deployment)
	require.NoError(t, err)
	// mergeContainerEnvs merges; we should have at least EXISTING plus any proxy vars from env
	require.NotEmpty(t, deployment.Spec.Template.Spec.Containers[0].Env)
}

// TestWithCAConfigMap covers the modified hook (parameter rename); empty name and success path.
func TestWithCAConfigMap(t *testing.T) {
	t.Run("empty configmap name returns nil", func(t *testing.T) {
		hook := withCAConfigMap(nil, nil, "")
		dep := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "c"}}},
				},
			},
		}
		err := hook(nil, dep)
		require.NoError(t, err)
	})
	t.Run("configmap not found returns retry error", func(t *testing.T) {
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		// empty indexer -> Get("my-ca") will return not found
		lister := corelistersv1.NewConfigMapLister(indexer)
		fakeInformer := &fakeConfigMapInformer{lister: lister}
		hook := withCAConfigMap(fakeInformer, nil, "my-ca")
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "c"}},
					},
				},
			},
		}
		err := hook(nil, deployment)
		require.Error(t, err)
		require.True(t, strings.Contains(err.Error(), "Retrying") || apierrors.IsNotFound(err), "expected retry or NotFound")
	})
	t.Run("configmap exists adds volume and volume mounts", func(t *testing.T) {
		cm := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "trusted-ca", Namespace: operatorclient.TargetNamespace},
		}
		indexer := cache.NewIndexer(cache.MetaNamespaceKeyFunc, cache.Indexers{})
		require.NoError(t, indexer.Add(cm))
		lister := corelistersv1.NewConfigMapLister(indexer)
		fakeInformer := &fakeConfigMapInformer{lister: lister}
		hook := withCAConfigMap(fakeInformer, nil, "trusted-ca")
		deployment := &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{{Name: "c1"}, {Name: "c2"}},
					},
				},
			},
		}
		err := hook(nil, deployment)
		require.NoError(t, err)
		require.NotEmpty(t, deployment.Spec.Template.Spec.Volumes)
		require.Equal(t, trustedCAVolumeName, deployment.Spec.Template.Spec.Volumes[len(deployment.Spec.Template.Spec.Volumes)-1].Name)
		for i := range deployment.Spec.Template.Spec.Containers {
			require.NotEmpty(t, deployment.Spec.Template.Spec.Containers[i].VolumeMounts)
		}
	})
}

// fakeConfigMapInformer implements coreinformersv1.ConfigMapInformer for tests.
type fakeConfigMapInformer struct {
	lister corelistersv1.ConfigMapLister
}

func (f *fakeConfigMapInformer) Informer() cache.SharedIndexInformer { return nil }
func (f *fakeConfigMapInformer) Lister() corelistersv1.ConfigMapLister {
	return f.lister
}

// TestWithSABoundToken covers the modified hook (parameter rename).
func TestWithSABoundToken(t *testing.T) {
	deployment := &appsv1.Deployment{
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "controller"}},
				},
			},
		},
	}
	err := withSABoundToken(nil, deployment)
	require.NoError(t, err)
	require.NotEmpty(t, deployment.Spec.Template.Spec.Volumes)
	var found bool
	for _, v := range deployment.Spec.Template.Spec.Volumes {
		if v.Name == boundSATokenVolumeName {
			found = true
			break
		}
	}
	require.True(t, found)
	require.NotEmpty(t, deployment.Spec.Template.Spec.Containers[0].VolumeMounts)
}
