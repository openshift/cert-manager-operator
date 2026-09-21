package http01proxy

import (
	"context"
	"fmt"
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
