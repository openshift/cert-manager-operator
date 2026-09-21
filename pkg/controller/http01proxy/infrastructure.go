package http01proxy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"

	configv1 "github.com/openshift/api/config/v1"
)

const (
	platformBareMetal = string(configv1.BareMetalPlatformType)
	platformUnknown   = "Unknown"
)

// platformInfo holds the discovered platform details needed to decide
// whether the HTTP01 proxy should be deployed.
type platformInfo struct {
	platformType string
	apiVIPs      []string
	ingressVIPs  []string
}

// discoverPlatform reads the Infrastructure CR and returns platform details.
func (r *Reconciler) discoverPlatform(ctx context.Context) (*platformInfo, error) {
	infra := &configv1.Infrastructure{}
	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, infra); err != nil {
		if errors.IsNotFound(err) || meta.IsNoMatchError(err) {
			return &platformInfo{platformType: platformUnknown}, nil
		}
		return nil, fmt.Errorf("failed to get infrastructure/cluster: %w", err)
	}

	if infra.Status.PlatformStatus == nil {
		return nil, fmt.Errorf("infrastructure status.platformStatus not found")
	}

	platformType := string(infra.Status.PlatformStatus.Type)
	info := &platformInfo{
		platformType: platformType,
	}

	switch platformType {
	case platformBareMetal:
		if infra.Status.PlatformStatus.BareMetal == nil {
			return info, nil
		}
		info.apiVIPs = append([]string(nil), infra.Status.PlatformStatus.BareMetal.APIServerInternalIPs...)
		info.ingressVIPs = append([]string(nil), infra.Status.PlatformStatus.BareMetal.IngressIPs...)
	}

	return info, nil
}

// validatePlatform checks whether the platform supports HTTP01 proxy deployment.
func validatePlatform(info *platformInfo) error {
	if info.platformType != platformBareMetal {
		return fmt.Errorf("platform type %q is not supported; HTTP01 proxy is only supported on BareMetal platforms", info.platformType)
	}

	if len(info.apiVIPs) == 0 {
		return fmt.Errorf("no API server VIPs found in infrastructure status; cannot deploy HTTP01 proxy")
	}

	if len(info.ingressVIPs) == 0 {
		return fmt.Errorf("no ingress VIPs found in infrastructure status; cannot deploy HTTP01 proxy")
	}

	for _, apiVIP := range info.apiVIPs {
		for _, ingressVIP := range info.ingressVIPs {
			if apiVIP == ingressVIP {
				return fmt.Errorf("API VIP and ingress VIP are the same; HTTP01 proxy is not needed")
			}
		}
	}

	return nil
}
