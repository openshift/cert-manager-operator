package http01proxy

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"

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
	if err := r.Get(ctx, types.NamespacedName{Name: infrastructureObjectName}, infra); err != nil {
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

	apiAddrs, err := parseVIPs(info.apiVIPs)
	if err != nil {
		return fmt.Errorf("invalid API VIP: %w", err)
	}
	ingressAddrs, err := parseVIPs(info.ingressVIPs)
	if err != nil {
		return fmt.Errorf("invalid ingress VIP: %w", err)
	}
	// Compare parsed addresses so the same VIP written differently (e.g.
	// "fd00::1" and "FD00:0::1") is still caught.
	for _, apiAddr := range apiAddrs {
		if slices.Contains(ingressAddrs, apiAddr) {
			return fmt.Errorf("API VIP and ingress VIP are the same; HTTP01 proxy is not needed")
		}
	}

	if _, err := vipFamiliesFor(info.apiVIPs, info.ingressVIPs); err != nil {
		return err
	}

	return nil
}

// vipFamily holds the API/ingress VIP pair of one IP family together with the
// family-specific values the nftables rules and systemd unit are rendered with.
type vipFamily struct {
	NFTFamily     string
	IPTables      string
	HostPrefixLen int
	ForwardSysctl string
	APIVIP        string
	IngressVIP    string
}

// IngressDNATTarget returns the ingress VIP and port in the form nftables
// expects as a dnat target; IPv6 addresses are bracketed.
func (f vipFamily) IngressDNATTarget() string {
	return net.JoinHostPort(f.IngressVIP, "80")
}

// ForwardRequestRule returns the iptables FORWARD rule spec that accepts
// DNATed requests to the ingress VIP.
func (f vipFamily) ForwardRequestRule() string {
	return fmt.Sprintf("-p tcp -d %s/%d --dport 80 -j ACCEPT", f.IngressVIP, f.HostPrefixLen)
}

// ForwardReplyRule returns the iptables FORWARD rule spec that accepts the
// router's replies from the ingress VIP. It is needed where the FORWARD policy
// is DROP, as OVN-Kubernetes sets for IPv6 in restricted forwarding mode.
func (f vipFamily) ForwardReplyRule() string {
	return fmt.Sprintf("-p tcp -s %s/%d --sport 80 -m conntrack --ctstate ESTABLISHED,RELATED -j ACCEPT", f.IngressVIP, f.HostPrefixLen)
}

// vipFamiliesFor pairs the API and ingress VIPs by IP family. Dual-stack
// clusters list one VIP per family, in either order; the result is always
// IPv4 first so the rendered MachineConfig does not depend on that order.
// A family missing from either list is skipped rather than treated as an
// error, since a validation failure removes the MachineConfig from all
// control plane nodes.
func vipFamiliesFor(apiVIPs, ingressVIPs []string) ([]vipFamily, error) {
	api4, api6, err := firstVIPPerFamily(apiVIPs)
	if err != nil {
		return nil, fmt.Errorf("invalid API VIP: %w", err)
	}
	ingress4, ingress6, err := firstVIPPerFamily(ingressVIPs)
	if err != nil {
		return nil, fmt.Errorf("invalid ingress VIP: %w", err)
	}

	var families []vipFamily
	if api4.IsValid() && ingress4.IsValid() {
		families = append(families, vipFamily{
			NFTFamily:     "ip",
			IPTables:      "/usr/sbin/iptables",
			HostPrefixLen: 32,
			ForwardSysctl: "net.ipv4.ip_forward",
			APIVIP:        api4.String(),
			IngressVIP:    ingress4.String(),
		})
	}
	if api6.IsValid() && ingress6.IsValid() {
		families = append(families, vipFamily{
			NFTFamily:     "ip6",
			IPTables:      "/usr/sbin/ip6tables",
			HostPrefixLen: 128,
			ForwardSysctl: "net.ipv6.conf.all.forwarding",
			APIVIP:        api6.String(),
			IngressVIP:    ingress6.String(),
		})
	}
	if len(families) == 0 {
		return nil, fmt.Errorf("API VIPs %v and ingress VIPs %v share no IP family; cannot deploy HTTP01 proxy", apiVIPs, ingressVIPs)
	}
	return families, nil
}

// parseVIPs parses vips into addresses, unmapping IPv4-mapped IPv6 addresses
// to IPv4. Zoned IPv6 addresses (e.g. "fe80::1%br-ex") are rejected: a VIP is
// not link-local and nftables does not accept a zone in an address.
func parseVIPs(vips []string) ([]netip.Addr, error) {
	addrs := make([]netip.Addr, 0, len(vips))
	for _, vip := range vips {
		addr, err := netip.ParseAddr(vip)
		if err != nil {
			return nil, fmt.Errorf("parse %q: %w", vip, err)
		}
		if addr.Zone() != "" {
			return nil, fmt.Errorf("%q has an IPv6 zone", vip)
		}
		addrs = append(addrs, addr.Unmap())
	}
	return addrs, nil
}

// firstVIPPerFamily returns the first IPv4 and the first IPv6 address in vips;
// either is the zero netip.Addr when the family is absent.
func firstVIPPerFamily(vips []string) (v4, v6 netip.Addr, err error) {
	addrs, err := parseVIPs(vips)
	if err != nil {
		return netip.Addr{}, netip.Addr{}, err
	}
	for _, addr := range addrs {
		switch {
		case addr.Is4() && !v4.IsValid():
			v4 = addr
		case addr.Is6() && !v6.IsValid():
			v6 = addr
		}
	}
	return v4, v6, nil
}
