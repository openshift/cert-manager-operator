package library

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
)

func NewClientsConfigForTest(t *testing.T) (kubernetes.Interface, dynamic.Interface) {
	config, err := GetConfigForTest(t)
	if err == nil {
		t.Logf("Found configuration for host %v.\n", config.Host)
	}

	require.NoError(t, err)
	kubeClient, err := kubernetes.NewForConfig(config)
	require.NoError(t, err)
	dynamicKubeConfig, err := dynamic.NewForConfig(config)
	require.NoError(t, err)
	return kubeClient, dynamicKubeConfig
}

func GetConfigForTest(t *testing.T) (*rest.Config, error) {
	loader := clientcmd.NewDefaultClientConfigLoadingRules()
	clientConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loader, &clientcmd.ConfigOverrides{ClusterInfo: api.Cluster{InsecureSkipTLSVerify: true}})
	config, err := clientConfig.ClientConfig()
	if err == nil {
		t.Logf("Found configuration for host %v.\n", config.Host)
	}

	require.NoError(t, err)
	return config, err
}
