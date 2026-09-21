package tlsprofile

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	libgocrypto "github.com/openshift/library-go/pkg/crypto"
)

// APIServerClusterName is the singleton apiserver.config.openshift.io object.
const APIServerClusterName = "cluster"

// FetchAPIServerFunc retrieves apiserver.config.openshift.io/cluster.
type FetchAPIServerFunc func(ctx context.Context) (*configv1.APIServer, error)

// FetchErrorMode controls how ResolveHonoredTLSProfile treats fetch failures.
type FetchErrorMode int

const (
	// FetchErrorPropagateExceptNotFound returns NotFound as a soft skip and
	// propagates all other fetch errors.
	FetchErrorPropagateExceptNotFound FetchErrorMode = iota
)

// ObjectGetter is the Get subset used to fetch APIServer. It matches
// common.CtrlClient's Get signature (no GetOption variadic), which is what
// production callers pass via NewClientReaderAPIServerFetch.
type ObjectGetter interface {
	Get(ctx context.Context, key client.ObjectKey, obj client.Object) error
}

// NewRESTConfigAPIServerFetch returns a client-go fetcher for the cluster APIServer.
func NewRESTConfigAPIServerFetch(restConfig *rest.Config) (FetchAPIServerFunc, error) {
	if restConfig == nil {
		return nil, fmt.Errorf("rest config is nil")
	}
	configClient, err := configv1client.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create config client: %w", err)
	}
	return func(ctx context.Context) (*configv1.APIServer, error) {
		return configClient.ConfigV1().APIServers().Get(ctx, APIServerClusterName, metav1.GetOptions{})
	}, nil
}

// NewClientReaderAPIServerFetch returns a controller-runtime fetcher for the cluster APIServer.
func NewClientReaderAPIServerFetch(r ObjectGetter) FetchAPIServerFunc {
	return func(ctx context.Context) (*configv1.APIServer, error) {
		apiServer := &configv1.APIServer{}
		if err := r.Get(ctx, types.NamespacedName{Name: APIServerClusterName}, apiServer); err != nil {
			return nil, err
		}
		return apiServer, nil
	}
}

// ResolveHonoredTLSProfile fetches the cluster APIServer via fetch and, when
// tlsAdherence requires enforcement, returns EffectiveSpec. A nil profile with
// a nil error means the caller should leave existing TLS settings unchanged.
func ResolveHonoredTLSProfile(ctx context.Context, fetch FetchAPIServerFunc, component string, mode FetchErrorMode) (*configv1.TLSProfileSpec, error) {
	if fetch == nil {
		return nil, fmt.Errorf("APIServer fetch function is nil")
	}

	apiServer, err := fetch(ctx)
	if err != nil {
		switch mode {
		case FetchErrorPropagateExceptNotFound:
			if apierrors.IsNotFound(err) {
				klog.V(4).Infof("skipping cluster TLS profile for %s: apiserver.config.openshift.io/cluster not found", component)
				return nil, nil
			}
			return nil, fmt.Errorf("failed to get apiserver.config.openshift.io/cluster: %w", err)
		default:
			return nil, fmt.Errorf("failed to get apiserver.config.openshift.io/cluster: %w", err)
		}
	}

	adherence := apiServer.Spec.TLSAdherence
	if !libgocrypto.ShouldHonorClusterTLSProfile(adherence) {
		klog.V(4).Infof("skipping cluster TLS profile for %s: apiserver tlsAdherence=%q", component, adherence)
		return nil, nil
	}
	if adherence != configv1.TLSAdherencePolicyStrictAllComponents {
		klog.Warningf("apiserver.config.openshift.io/cluster has unknown tlsAdherence %q; treating as StrictAllComponents for %s", adherence, component)
	}

	return EffectiveSpec(apiServer.Spec.TLSSecurityProfile)
}

// ClusterAPIServerTLSConfigChanged reports whether TLS fields that this
// operator honors changed between old and new. Status and unrelated spec
// updates are ignored. A nil/non-nil transition is treated as a change.
func ClusterAPIServerTLSConfigChanged(old, new *configv1.APIServer) bool {
	if old == nil || new == nil {
		return old != new
	}
	return old.Spec.TLSAdherence != new.Spec.TLSAdherence ||
		!equality.Semantic.DeepEqual(old.Spec.TLSSecurityProfile, new.Spec.TLSSecurityProfile)
}
