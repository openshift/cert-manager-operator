package tlsprofile

import (
	"context"
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
)

// ApplyClusterProfileToHTTPServingInfo reads apiserver.config.openshift.io/cluster
// and, when tlsAdherence requires enforcement, applies the effective TLS profile
// to serving. A missing APIServer (NotFound) or non-enforcing adherence leaves
// serving unchanged; other APIServer lookup failures are returned.
func ApplyClusterProfileToHTTPServingInfo(ctx context.Context, restConfig *rest.Config, serving *configv1.HTTPServingInfo) error {
	if serving == nil {
		return fmt.Errorf("HTTPServingInfo is nil")
	}

	fetch, err := NewRESTConfigAPIServerFetch(restConfig)
	if err != nil {
		return err
	}

	effective, err := ResolveHonoredTLSProfile(ctx, fetch, "operator serving", FetchErrorPropagateExceptNotFound)
	if err != nil {
		return err
	}
	if effective == nil {
		return nil
	}

	if err := ApplyToHTTPServingInfo(serving, effective); err != nil {
		return err
	}
	klog.V(2).Infof("applied cluster TLS profile to operator serving: minTLSVersion=%s ciphers=%d", serving.MinTLSVersion, len(serving.CipherSuites))
	return nil
}

// RESTConfigFromKubeConfig returns an in-cluster rest.Config when kubeConfigFile
// is empty, otherwise loads the given kubeconfig path.
func RESTConfigFromKubeConfig(kubeConfigFile string) (*rest.Config, error) {
	if len(kubeConfigFile) == 0 {
		return rest.InClusterConfig()
	}
	return clientcmd.BuildConfigFromFlags("", kubeConfigFile)
}
