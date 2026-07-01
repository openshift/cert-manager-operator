package http01proxy

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// platformBareMetal is the platform type for baremetal clusters.
	platformBareMetal = "BareMetal"
)

// platformInfo holds the discovered platform details needed to decide
// whether the HTTP01 proxy should be deployed.
type platformInfo struct {
	platformType string
	apiVIPs      []string
	ingressVIPs  []string
}

// getOrDiscoverPlatform returns cached platform info, or fetches it on first call.
func (r *Reconciler) getOrDiscoverPlatform(ctx context.Context) (*platformInfo, error) {
	r.platformMu.Lock()
	defer r.platformMu.Unlock()
	if r.cachedPlatform != nil {
		return r.cachedPlatform, nil
	}
	info, err := r.discoverPlatform(ctx)
	if err != nil {
		return nil, err
	}
	r.cachedPlatform = info
	return info, nil
}

// discoverPlatform reads the Infrastructure CR and returns platform details.
func (r *Reconciler) discoverPlatform(ctx context.Context) (*platformInfo, error) {
	infra := &unstructured.Unstructured{}
	infra.SetGroupVersionKind(infrastructureGVK)

	if err := r.Get(ctx, types.NamespacedName{Name: "cluster"}, infra); err != nil {
		return nil, fmt.Errorf("failed to get infrastructure/cluster: %w", err)
	}

	platformType, found, err := unstructured.NestedString(infra.Object, "status", "platformStatus", "type")
	if err != nil {
		return nil, fmt.Errorf("failed to parse infrastructure status.platformStatus.type: %w", err)
	}
	if !found {
		return nil, fmt.Errorf("infrastructure status.platformStatus.type not found")
	}

	info := &platformInfo{
		platformType: platformType,
	}

	switch platformType {
	case platformBareMetal:
		apiVIPs, _, err := unstructured.NestedStringSlice(infra.Object, "status", "platformStatus", "baremetal", "apiServerInternalIPs")
		if err != nil {
			return nil, fmt.Errorf("failed to parse baremetal.apiServerInternalIPs: %w", err)
		}
		ingressVIPs, _, err := unstructured.NestedStringSlice(infra.Object, "status", "platformStatus", "baremetal", "ingressIPs")
		if err != nil {
			return nil, fmt.Errorf("failed to parse baremetal.ingressIPs: %w", err)
		}
		info.apiVIPs = apiVIPs
		info.ingressVIPs = ingressVIPs
	}

	return info, nil
}

// validatePlatform checks whether the platform supports HTTP01 proxy deployment.
// Returns a human-readable reason if the platform is not supported, or empty string if OK.
func validatePlatform(info *platformInfo) string {
	if info.platformType != platformBareMetal {
		return fmt.Sprintf("platform type %q is not supported; HTTP01 proxy is only supported on BareMetal platforms", info.platformType)
	}

	if len(info.apiVIPs) == 0 {
		return "no API server VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
	}

	if len(info.ingressVIPs) == 0 {
		return "no ingress VIPs found in infrastructure status; cannot deploy HTTP01 proxy"
	}

	// If any API VIP equals any ingress VIP, proxy is not needed
	for _, apiVIP := range info.apiVIPs {
		for _, ingressVIP := range info.ingressVIPs {
			if apiVIP == ingressVIP {
				return fmt.Sprintf("API VIP (%s) and ingress VIP (%s) are the same; HTTP01 proxy is not needed", apiVIP, ingressVIP)
			}
		}
	}

	return ""
}
