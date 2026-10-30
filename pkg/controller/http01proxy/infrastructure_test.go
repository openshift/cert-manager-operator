package http01proxy

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"

	"github.com/openshift/cert-manager-operator/pkg/controller/common/fakes"
)

func TestValidatePlatform(t *testing.T) {
	tests := []struct {
		name      string
		info      *platformInfo
		wantMsg   string
		wantEmpty bool
	}{
		{
			name:    "non-baremetal platform",
			info:    &platformInfo{platformType: "AWS"},
			wantMsg: "not supported",
		},
		{
			name:    "baremetal no API VIPs",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: nil, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "no API server VIPs",
		},
		{
			name:    "baremetal no ingress VIPs",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1"}, ingressVIPs: nil},
			wantMsg: "no ingress VIPs",
		},
		{
			name:    "baremetal overlapping VIPs",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1"}, ingressVIPs: []string{"10.0.0.1"}},
			wantMsg: "are the same",
		},
		{
			name:      "baremetal valid distinct VIPs",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1"}, ingressVIPs: []string{"10.0.0.2"}},
			wantEmpty: true,
		},
		{
			name:      "baremetal multiple distinct VIPs",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1", "fd00::1"}, ingressVIPs: []string{"10.0.0.2", "fd00::2"}},
			wantEmpty: true,
		},
		{
			name:    "baremetal one overlapping pair among multiple",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1", "10.0.0.3"}, ingressVIPs: []string{"10.0.0.2", "10.0.0.1"}},
			wantMsg: "are the same",
		},
		{
			name:      "baremetal IPv6-only VIPs",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"fd00::2"}},
			wantEmpty: true,
		},
		{
			name:    "baremetal overlapping IPv6 VIPs in dual-stack",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1", "fd00::1"}, ingressVIPs: []string{"10.0.0.2", "fd00::1"}},
			wantMsg: "are the same",
		},
		{
			name:    "baremetal invalid VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"not-an-ip"}, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "invalid API VIP",
		},
		{
			name:    "baremetal VIPs with no common family",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1"}, ingressVIPs: []string{"fd00::2"}},
			wantMsg: "share no IP family",
		},
		{
			name:    "baremetal overlapping IPv6-only VIPs",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"fd00::1"}},
			wantMsg: "are the same",
		},
		{
			name:    "baremetal overlapping IPv4 VIPs in dual-stack",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1", "fd00::1"}, ingressVIPs: []string{"10.0.0.1", "fd00::2"}},
			wantMsg: "are the same",
		},
		{
			name:    "baremetal invalid IPv6 ingress VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"fd00::zz"}},
			wantMsg: "invalid ingress VIP",
		},
		{
			name:    "baremetal same IPv6 VIP in different notation",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"FD00:0:0::1"}},
			wantMsg: "are the same",
		},
		{
			name:    "baremetal same IPv4 VIP as IPv4-mapped IPv6",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"::ffff:10.0.0.1"}, ingressVIPs: []string{"10.0.0.1"}},
			wantMsg: "are the same",
		},
		{
			name:    "baremetal same IPv6 VIP in different notation among multiple",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1", "fd00::1"}, ingressVIPs: []string{"10.0.0.2", "fd00:0::1"}},
			wantMsg: "are the same",
		},
		{
			name:    "baremetal zoned IPv6 API VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fe80::1%br-ex"}, ingressVIPs: []string{"fe80::2"}},
			wantMsg: "invalid API VIP",
		},
		{
			name:    "baremetal zoned IPv6 ingress VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"fd00::2%eth0"}},
			wantMsg: "invalid ingress VIP",
		},
		{
			name:    "baremetal invalid IPv4 ingress VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1"}, ingressVIPs: []string{"10.0.0.256"}},
			wantMsg: "invalid ingress VIP",
		},
		{
			name:    "baremetal invalid IPv6 API VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1::2"}, ingressVIPs: []string{"fd00::2"}},
			wantMsg: "invalid API VIP",
		},
		{
			name:    "baremetal whitespace-padded VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{" 10.0.0.1"}, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "invalid API VIP",
		},
		{
			name:    "baremetal empty-string VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{""}, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "invalid API VIP",
		},
		{
			name:    "baremetal hostname instead of VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"api.example.com"}, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "invalid API VIP",
		},
		{
			name:      "baremetal dual-stack with IPv6 listed first",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1", "10.0.0.1"}, ingressVIPs: []string{"fd00::2", "10.0.0.2"}},
			wantEmpty: true,
		},
		{
			name:      "baremetal IPv6 only in API VIPs still valid via IPv4",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1", "fd00::1"}, ingressVIPs: []string{"10.0.0.2"}},
			wantEmpty: true,
		},
		{
			name:      "baremetal IPv4 only in ingress VIPs still valid via IPv6",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"10.0.0.2", "fd00::2"}},
			wantEmpty: true,
		},
		{
			name:      "baremetal link-local IPv6 VIPs without zone",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fe80::1"}, ingressVIPs: []string{"fe80::2"}},
			wantEmpty: true,
		},
		{
			name:      "baremetal global IPv6 VIPs",
			info:      &platformInfo{platformType: "BareMetal", apiVIPs: []string{"2620:52:0:2e61::1632"}, ingressVIPs: []string{"2620:52:0:2e61::1648"}},
			wantEmpty: true,
		},
		{
			name:    "baremetal no ingress VIPs on IPv6",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: nil},
			wantMsg: "no ingress VIPs",
		},
		{
			name:    "baremetal no API VIPs on IPv6",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: nil, ingressVIPs: []string{"fd00::2"}},
			wantMsg: "no API server VIPs",
		},
		{
			name:    "baremetal IPv6 API VIP with IPv4 ingress VIP",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{"fd00::1"}, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "share no IP family",
		},
		{
			name:    "empty platform type",
			info:    &platformInfo{platformType: ""},
			wantMsg: "not supported",
		},
		{
			name:    "None platform type",
			info:    &platformInfo{platformType: "None"},
			wantMsg: "not supported",
		},
		{
			name:    "baremetal empty VIP slices",
			info:    &platformInfo{platformType: "BareMetal", apiVIPs: []string{}, ingressVIPs: []string{"10.0.0.2"}},
			wantMsg: "no API server VIPs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := validatePlatform(tt.info)
			if tt.wantEmpty {
				if got != nil {
					t.Errorf("validatePlatform() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Error("validatePlatform() = nil, want non-nil error")
				return
			}
			if !strings.Contains(got.Error(), tt.wantMsg) {
				t.Errorf("validatePlatform() = %q, want substring %q", got.Error(), tt.wantMsg)
			}
		})
	}
}

func TestVIPFamiliesFor(t *testing.T) {
	ipv4 := vipFamily{
		NFTFamily:     "ip",
		IPTables:      "/usr/sbin/iptables",
		HostPrefixLen: 32,
		ForwardSysctl: "net.ipv4.ip_forward",
		APIVIP:        "10.0.0.1",
		IngressVIP:    "10.0.0.2",
	}
	ipv6 := vipFamily{
		NFTFamily:     "ip6",
		IPTables:      "/usr/sbin/ip6tables",
		HostPrefixLen: 128,
		ForwardSysctl: "net.ipv6.conf.all.forwarding",
		APIVIP:        "fd00::1",
		IngressVIP:    "fd00::2",
	}

	tests := []struct {
		name        string
		apiVIPs     []string
		ingressVIPs []string
		want        []vipFamily
		wantErr     string
	}{
		{
			name:        "IPv4 only",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"10.0.0.2"},
			want:        []vipFamily{ipv4},
		},
		{
			name:        "IPv6 only",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"fd00::2"},
			want:        []vipFamily{ipv6},
		},
		{
			name:        "dual-stack IPv4 first",
			apiVIPs:     []string{"10.0.0.1", "fd00::1"},
			ingressVIPs: []string{"10.0.0.2", "fd00::2"},
			want:        []vipFamily{ipv4, ipv6},
		},
		{
			name:        "dual-stack IPv6 first",
			apiVIPs:     []string{"fd00::1", "10.0.0.1"},
			ingressVIPs: []string{"fd00::2", "10.0.0.2"},
			want:        []vipFamily{ipv4, ipv6},
		},
		{
			name:        "IPv6 present in only one list",
			apiVIPs:     []string{"10.0.0.1", "fd00::1"},
			ingressVIPs: []string{"10.0.0.2"},
			want:        []vipFamily{ipv4},
		},
		{
			name:        "IPv6 address is normalized",
			apiVIPs:     []string{"FD00:0:0::1"},
			ingressVIPs: []string{"fd00::2"},
			want:        []vipFamily{ipv6},
		},
		{
			name:        "IPv4-mapped IPv6 treated as IPv4",
			apiVIPs:     []string{"::ffff:10.0.0.1"},
			ingressVIPs: []string{"10.0.0.2"},
			want:        []vipFamily{ipv4},
		},
		{
			name:        "first VIP of a family wins",
			apiVIPs:     []string{"10.0.0.1", "10.0.0.9"},
			ingressVIPs: []string{"10.0.0.2"},
			want:        []vipFamily{ipv4},
		},
		{
			name:        "IPv4 present in only one list",
			apiVIPs:     []string{"10.0.0.1", "fd00::1"},
			ingressVIPs: []string{"fd00::2"},
			want:        []vipFamily{ipv6},
		},
		{
			name:        "first IPv6 VIP wins",
			apiVIPs:     []string{"fd00::1", "fd00::9"},
			ingressVIPs: []string{"fd00::2"},
			want:        []vipFamily{ipv6},
		},
		{
			name:        "no common family, IPv4 API and IPv6 ingress",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"fd00::2"},
			wantErr:     "share no IP family",
		},
		{
			name:        "no common family, IPv6 API and IPv4 ingress",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"10.0.0.2"},
			wantErr:     "share no IP family",
		},
		{
			name:        "invalid IPv4 API VIP",
			apiVIPs:     []string{"10.0.0.300"},
			ingressVIPs: []string{"10.0.0.2"},
			wantErr:     "invalid API VIP",
		},
		{
			name:        "invalid IPv4 ingress VIP",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"10.0.0"},
			wantErr:     "invalid ingress VIP",
		},
		{
			name:        "invalid IPv6 API VIP",
			apiVIPs:     []string{"fd00::zz"},
			ingressVIPs: []string{"fd00::2"},
			wantErr:     "invalid API VIP",
		},
		{
			name:        "invalid IPv6 ingress VIP",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"fd00:::2"},
			wantErr:     "invalid ingress VIP",
		},
		{
			name:        "IPv6 VIP with CIDR suffix is invalid",
			apiVIPs:     []string{"fd00::1/128"},
			ingressVIPs: []string{"fd00::2"},
			wantErr:     "invalid API VIP",
		},
		{
			name:        "non-IP ingress VIP",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"bogus"},
			wantErr:     "invalid ingress VIP",
		},
		{
			name:        "zoned IPv6 API VIP",
			apiVIPs:     []string{"fe80::1%br-ex"},
			ingressVIPs: []string{"fe80::2"},
			wantErr:     "IPv6 zone",
		},
		{
			name:        "zoned IPv6 ingress VIP",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"fd00::2%eth0"},
			wantErr:     "invalid ingress VIP",
		},
		{
			name:        "zoned IPv6 VIP alongside valid IPv4 still rejected",
			apiVIPs:     []string{"10.0.0.1", "fe80::1%br-ex"},
			ingressVIPs: []string{"10.0.0.2"},
			wantErr:     "IPv6 zone",
		},
		{
			name:        "IPv4 CIDR suffix is invalid",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"10.0.0.2/32"},
			wantErr:     "invalid ingress VIP",
		},
		{
			name:        "bracketed IPv6 is invalid",
			apiVIPs:     []string{"[fd00::1]"},
			ingressVIPs: []string{"fd00::2"},
			wantErr:     "invalid API VIP",
		},
		{
			name:        "IPv6 with port is invalid",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"[fd00::2]:80"},
			wantErr:     "invalid ingress VIP",
		},
		{
			name:        "empty lists",
			apiVIPs:     []string{},
			ingressVIPs: []string{},
			wantErr:     "share no IP family",
		},
		{
			name:        "nil lists",
			apiVIPs:     nil,
			ingressVIPs: nil,
			wantErr:     "share no IP family",
		},
		{
			name:        "IPv4 only in ingress list",
			apiVIPs:     []string{"fd00::1"},
			ingressVIPs: []string{"10.0.0.2", "fd00::2"},
			want:        []vipFamily{ipv6},
		},
		{
			name:        "dual-stack with extra VIPs per family",
			apiVIPs:     []string{"fd00::1", "10.0.0.1", "fd00::9", "10.0.0.9"},
			ingressVIPs: []string{"10.0.0.2", "10.0.0.8", "fd00::2", "fd00::8"},
			want:        []vipFamily{ipv4, ipv6},
		},
		{
			name:        "IPv6 expanded form is compressed",
			apiVIPs:     []string{"fd00:0000:0000:0000:0000:0000:0000:0001"},
			ingressVIPs: []string{"fd00:0:0:0:0:0:0:2"},
			want:        []vipFamily{ipv6},
		},
		{
			name:        "IPv4-mapped IPv6 ingress treated as IPv4",
			apiVIPs:     []string{"10.0.0.1"},
			ingressVIPs: []string{"::ffff:10.0.0.2"},
			want:        []vipFamily{ipv4},
		},
		{
			name:        "invalid VIP after a valid one of the other family",
			apiVIPs:     []string{"10.0.0.1", "fd00::g"},
			ingressVIPs: []string{"10.0.0.2", "fd00::2"},
			wantErr:     "invalid API VIP",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := vipFamiliesFor(tt.apiVIPs, tt.ingressVIPs)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("vipFamiliesFor() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("vipFamiliesFor() unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("vipFamiliesFor() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestVIPFamilyIngressDNATTarget(t *testing.T) {
	tests := []struct {
		ingressVIP string
		want       string
	}{
		{ingressVIP: "10.0.0.2", want: "10.0.0.2:80"},
		{ingressVIP: "fd00::2", want: "[fd00::2]:80"},
		{ingressVIP: "2620:52:0:2e61::1648", want: "[2620:52:0:2e61::1648]:80"},
		{ingressVIP: "fd00:1:2:3:4:5:6:7", want: "[fd00:1:2:3:4:5:6:7]:80"},
		{ingressVIP: "::1", want: "[::1]:80"},
	}
	for _, tt := range tests {
		if got := (vipFamily{IngressVIP: tt.ingressVIP}).IngressDNATTarget(); got != tt.want {
			t.Errorf("IngressDNATTarget(%q) = %q, want %q", tt.ingressVIP, got, tt.want)
		}
	}
}

func TestParseVIPs(t *testing.T) {
	tests := []struct {
		name    string
		vips    []string
		want    []string
		wantErr string
	}{
		{name: "nil", vips: nil, want: []string{}},
		{name: "IPv4", vips: []string{"10.0.0.1"}, want: []string{"10.0.0.1"}},
		{name: "IPv6", vips: []string{"fd00::1"}, want: []string{"fd00::1"}},
		{name: "mixed order preserved", vips: []string{"fd00::1", "10.0.0.1"}, want: []string{"fd00::1", "10.0.0.1"}},
		{name: "IPv4-mapped IPv6 unmapped", vips: []string{"::ffff:10.0.0.1"}, want: []string{"10.0.0.1"}},
		{name: "IPv6 normalized", vips: []string{"FD00:0:0::1"}, want: []string{"fd00::1"}},
		{name: "link-local without zone", vips: []string{"fe80::1"}, want: []string{"fe80::1"}},
		{name: "IPv6 zone rejected", vips: []string{"fe80::1%br-ex"}, wantErr: "IPv6 zone"},
		{name: "numeric IPv6 zone rejected", vips: []string{"fe80::1%2"}, wantErr: "IPv6 zone"},
		{name: "invalid IPv4", vips: []string{"10.0.0.256"}, wantErr: `parse "10.0.0.256"`},
		{name: "invalid IPv6", vips: []string{"fd00::zz"}, wantErr: `parse "fd00::zz"`},
		{name: "IPv4 CIDR", vips: []string{"10.0.0.1/32"}, wantErr: "parse"},
		{name: "IPv6 CIDR", vips: []string{"fd00::1/128"}, wantErr: "parse"},
		{name: "empty string", vips: []string{""}, wantErr: "parse"},
		{name: "hostname", vips: []string{"api.example.com"}, wantErr: "parse"},
		{name: "error after valid VIP", vips: []string{"10.0.0.1", "bogus"}, wantErr: `parse "bogus"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseVIPs(tt.vips)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("parseVIPs() error = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseVIPs() unexpected error: %v", err)
			}
			gotStrs := make([]string, 0, len(got))
			for _, a := range got {
				gotStrs = append(gotStrs, a.String())
			}
			if !reflect.DeepEqual(gotStrs, tt.want) {
				t.Errorf("parseVIPs() = %v, want %v", gotStrs, tt.want)
			}
		})
	}
}

func TestDiscoverPlatform(t *testing.T) {
	tests := []struct {
		name         string
		getStub      func(context.Context, client.ObjectKey, client.Object) error
		wantErr      bool
		wantErrMsg   string
		wantPlatform string
		wantAPIVIPs  int
	}{
		{
			name: "Get error",
			getStub: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
				return fmt.Errorf("connection refused")
			},
			wantErr:    true,
			wantErrMsg: "failed to get infrastructure/cluster",
		},
		{
			name: "Infrastructure NotFound returns Unknown",
			getStub: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
				return apierrors.NewNotFound(schema.GroupResource{Group: "config.openshift.io", Resource: "infrastructures"}, "cluster")
			},
			wantPlatform: platformUnknown,
			wantAPIVIPs:  0,
		},
		{
			name: "Infrastructure NoMatchError returns Unknown",
			getStub: func(_ context.Context, _ client.ObjectKey, _ client.Object) error {
				return &meta.NoResourceMatchError{PartialResource: schema.GroupVersionResource{Group: "config.openshift.io", Version: "v1", Resource: "infrastructures"}}
			},
			wantPlatform: platformUnknown,
			wantAPIVIPs:  0,
		},
		{
			name: "BareMetal with nil BareMetal status",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status.PlatformStatus = &configv1.PlatformStatus{
					Type:      configv1.BareMetalPlatformType,
					BareMetal: nil,
				}
				return nil
			},
			wantPlatform: "BareMetal",
			wantAPIVIPs:  0,
		},
		{
			name: "missing platformStatus",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status = configv1.InfrastructureStatus{}
				return nil
			},
			wantErr:    true,
			wantErrMsg: "not found",
		},
		{
			name: "non-BareMetal platform",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status.PlatformStatus = &configv1.PlatformStatus{
					Type: configv1.AWSPlatformType,
				}
				return nil
			},
			wantPlatform: "AWS",
			wantAPIVIPs:  0,
		},
		{
			name: "BareMetal with VIPs",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status.PlatformStatus = &configv1.PlatformStatus{
					Type: configv1.BareMetalPlatformType,
					BareMetal: &configv1.BareMetalPlatformStatus{
						APIServerInternalIPs: []string{"10.0.0.1"},
						IngressIPs:           []string{"10.0.0.2"},
					},
				}
				return nil
			},
			wantPlatform: "BareMetal",
			wantAPIVIPs:  1,
		},
		{
			name: "BareMetal with IPv6 VIPs",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status.PlatformStatus = &configv1.PlatformStatus{
					Type: configv1.BareMetalPlatformType,
					BareMetal: &configv1.BareMetalPlatformStatus{
						APIServerInternalIPs: []string{"fd00::1"},
						IngressIPs:           []string{"fd00::2"},
					},
				}
				return nil
			},
			wantPlatform: "BareMetal",
			wantAPIVIPs:  1,
		},
		{
			name: "BareMetal with dual-stack VIPs",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status.PlatformStatus = &configv1.PlatformStatus{
					Type: configv1.BareMetalPlatformType,
					BareMetal: &configv1.BareMetalPlatformStatus{
						APIServerInternalIPs: []string{"10.0.0.1", "fd00::1"},
						IngressIPs:           []string{"10.0.0.2", "fd00::2"},
					},
				}
				return nil
			},
			wantPlatform: "BareMetal",
			wantAPIVIPs:  2,
		},
		{
			name: "BareMetal without VIP fields",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				infra := obj.(*configv1.Infrastructure)
				infra.Status.PlatformStatus = &configv1.PlatformStatus{
					Type:      configv1.BareMetalPlatformType,
					BareMetal: &configv1.BareMetalPlatformStatus{},
				}
				return nil
			},
			wantPlatform: "BareMetal",
			wantAPIVIPs:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := &fakes.FakeCtrlClient{}
			fakeClient.GetStub = tt.getStub
			r := &Reconciler{
				CtrlClient: fakeClient,
				log:        logr.Discard(),
			}

			info, err := r.discoverPlatform(context.Background())
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if !strings.Contains(err.Error(), tt.wantErrMsg) {
					t.Errorf("error = %q, want substring %q", err.Error(), tt.wantErrMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if info.platformType != tt.wantPlatform {
				t.Errorf("platformType = %q, want %q", info.platformType, tt.wantPlatform)
			}
			if len(info.apiVIPs) != tt.wantAPIVIPs {
				t.Errorf("len(apiVIPs) = %d, want %d", len(info.apiVIPs), tt.wantAPIVIPs)
			}
		})
	}
}
