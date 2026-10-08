package tlsprofile

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"

	configv1 "github.com/openshift/api/config/v1"
)

// ApplyClusterProfileToHTTPServingInfo reads apiserver.config.openshift.io/cluster
// and, when tlsAdherence requires enforcement, applies the effective TLS profile
// to serving. The returned profile is the baseline SecurityProfileWatcher compares
// against, including when adherence does not honor the profile (serving is then
// left unchanged).
//
// A missing APIServer (NotFound) is treated as empty: Intermediate spec, empty
// adherence, serving unchanged. Other lookup failures are returned. An
// unresolvable profile fails startup when adherence requires honoring it, and
// is reported via ResolvedProfile.Unresolvable when it does not.
func ApplyClusterProfileToHTTPServingInfo(ctx context.Context, restConfig *rest.Config, serving *configv1.HTTPServingInfo) (ResolvedProfile, error) {
	if serving == nil {
		return ResolvedProfile{}, fmt.Errorf("HTTPServingInfo is nil")
	}

	fetch, err := NewRESTConfigAPIServerFetch(restConfig)
	if err != nil {
		return ResolvedProfile{}, err
	}

	apiServer, err := fetch(ctx)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return ResolvedProfile{}, fmt.Errorf("failed to get apiserver.config.openshift.io/cluster: %w", err)
		}
		klog.V(4).Info("apiserver.config.openshift.io/cluster not found; leaving operator serving unchanged")
		apiServer = &configv1.APIServer{}
	}

	resolved, apply, err := profileForOperatorServing(apiServer)
	if err != nil {
		return ResolvedProfile{}, err
	}
	if !apply {
		return resolved, nil
	}

	if err := ApplyToHTTPServingInfo(serving, &resolved.Spec); err != nil {
		return ResolvedProfile{}, err
	}
	klog.V(2).Infof("applied cluster TLS profile to operator serving: minTLSVersion=%s ciphers=%d", serving.MinTLSVersion, len(serving.CipherSuites))
	return resolved, nil
}

// profileForOperatorServing decides whether the operator metrics server should
// adopt apiServer's TLS profile. apply is false when serving must stay at
// Controllercmd defaults.
func profileForOperatorServing(apiServer *configv1.APIServer) (ResolvedProfile, bool, error) {
	resolved, err := ResolveFromAPIServer(apiServer)
	if err != nil {
		if !resolved.Honor {
			resolved.Unresolvable = true
			klog.Warningf("cluster TLS profile is unresolvable (%v) but tlsAdherence=%q does not require honoring it; leaving operator serving unchanged", err, resolved.Adherence)
			return resolved, false, nil
		}
		return ResolvedProfile{}, false, err
	}
	if !resolved.Honor {
		klog.V(4).Infof("skipping cluster TLS profile for operator serving: apiserver tlsAdherence=%q", resolved.Adherence)
		return resolved, false, nil
	}
	return resolved, true, nil
}

// RESTConfigFromKubeConfig returns an in-cluster rest.Config when kubeConfigFile
// is empty, otherwise loads the given kubeconfig path.
func RESTConfigFromKubeConfig(kubeConfigFile string) (*rest.Config, error) {
	if len(kubeConfigFile) == 0 {
		return rest.InClusterConfig()
	}
	return clientcmd.BuildConfigFromFlags("", kubeConfigFile)
}
