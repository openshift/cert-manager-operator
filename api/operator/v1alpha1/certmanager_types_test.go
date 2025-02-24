package v1alpha1

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var testEnv = &envtest.Environment{
	CRDDirectoryPaths:     []string{filepath.Join("../../..", "config", "crd", "bases")},
	ErrorIfCRDPathMissing: true,
}

func skipIfNoEnvTest(t *testing.T) {
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("skipping envtest as KUBEBUILDER_ASSETS env var not found.")
	}
}

// TestTechPreviewFeatureValidation runs on an envtest with the CertManager CRD(s),
// to enforce CEL validation rules were honored.
func TestTechPreviewFeatureValidation(t *testing.T) {
	skipIfNoEnvTest(t)

	cfg, err := testEnv.Start()
	require.NoError(t, err)

	dynamicClient, err := dynamic.NewForConfig(cfg)
	require.NoError(t, err)

	resourceClient := dynamicClient.Resource(SchemeGroupVersion.WithResource("certmanagers"))

	t.Run("tech preview features cannot be unset once set", func(t *testing.T) {
		// creates certmanager CRD with tech preview features
		_, err = resourceClient.Create(context.TODO(), &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "operator.openshift.io/v1alpha1",
				"kind":       "CertManager",
				"metadata": map[string]interface{}{
					"name": "cluster",
				},
				"spec": map[string]interface{}{
					"unsupportedFeatures": map[string]interface{}{
						"techPreview": []string{
							"IstioCSR",
						},
					},
				},
			},
		}, metav1.CreateOptions{})
		require.NoError(t, err)

		t.Run("attempt to remove the features field from the spec cause error", func(t *testing.T) {
			_, err = resourceClient.Apply(context.TODO(), "cluster", &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "operator.openshift.io/v1alpha1",
					"kind":       "CertManager",
					"metadata": map[string]interface{}{
						"name": "cluster",
					},
					"spec": map[string]interface{}{},
				},
			}, metav1.ApplyOptions{})
			require.Error(t, err)
		})

		t.Run("disabling an already enabled feature cause error", func(t *testing.T) {
			_, err = resourceClient.Apply(context.TODO(), "cluster", &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "operator.openshift.io/v1alpha1",
					"kind":       "CertManager",
					"metadata": map[string]interface{}{
						"name": "cluster",
					},
					"spec": map[string]interface{}{
						"unsupportedFeatures": map[string]interface{}{
							"techPreview": []string{},
						},
					},
				},
			}, metav1.ApplyOptions{})
			require.Error(t, err)
		})

		err = resourceClient.Delete(context.TODO(), "cluster", metav1.DeleteOptions{})
		require.NoError(t, err)
	})

	t.Run("tech preview features cannot contain duplicates", func(t *testing.T) {
		// creates certmanager CRD with tech preview features
		_, err = resourceClient.Create(context.TODO(), &unstructured.Unstructured{
			Object: map[string]interface{}{
				"apiVersion": "operator.openshift.io/v1alpha1",
				"kind":       "CertManager",
				"metadata": map[string]interface{}{
					"name": "cluster",
				},
				"spec": map[string]interface{}{
					"unsupportedFeatures": map[string]interface{}{
						"techPreview": []string{
							"IstioCSR",
							"IstioCSR",
						},
					},
				},
			},
		}, metav1.CreateOptions{})
		require.Error(t, err)
	})

	err = testEnv.Stop()
	require.NoError(t, err)
}
