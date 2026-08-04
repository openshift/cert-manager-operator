package http01proxy

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"

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
				if got != "" {
					t.Errorf("validatePlatform() = %q, want empty string", got)
				}
				return
			}
			if got == "" {
				t.Error("validatePlatform() = empty string, want non-empty error message")
				return
			}
			if !strings.Contains(got, tt.wantMsg) {
				t.Errorf("validatePlatform() = %q, want substring %q", got, tt.wantMsg)
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
			name: "missing platformStatus.type",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				u := obj.(*unstructured.Unstructured)
				u.Object = map[string]interface{}{
					"status": map[string]interface{}{},
				}
				return nil
			},
			wantErr:    true,
			wantErrMsg: "not found",
		},
		{
			name: "non-BareMetal platform",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				u := obj.(*unstructured.Unstructured)
				u.Object = map[string]interface{}{
					"status": map[string]interface{}{
						"platformStatus": map[string]interface{}{
							"type": "AWS",
						},
					},
				}
				return nil
			},
			wantPlatform: "AWS",
			wantAPIVIPs:  0,
		},
		{
			name: "BareMetal with VIPs",
			getStub: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
				u := obj.(*unstructured.Unstructured)
				u.Object = map[string]interface{}{
					"status": map[string]interface{}{
						"platformStatus": map[string]interface{}{
							"type": "BareMetal",
							"baremetal": map[string]interface{}{
								"apiServerInternalIPs": []interface{}{"10.0.0.1"},
								"ingressIPs":           []interface{}{"10.0.0.2"},
							},
						},
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
				u := obj.(*unstructured.Unstructured)
				u.Object = map[string]interface{}{
					"status": map[string]interface{}{
						"platformStatus": map[string]interface{}{
							"type":      "BareMetal",
							"baremetal": map[string]interface{}{},
						},
					},
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

func TestGetOrDiscoverPlatform(t *testing.T) {
	t.Run("returns cached platform without calling Get", func(t *testing.T) {
		fakeClient := &fakes.FakeCtrlClient{}
		cached := &platformInfo{platformType: "BareMetal", apiVIPs: []string{"10.0.0.1"}, ingressVIPs: []string{"10.0.0.2"}}
		r := &Reconciler{
			CtrlClient:     fakeClient,
			log:            logr.Discard(),
			cachedPlatform: cached,
		}

		info, err := r.getOrDiscoverPlatform(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info != cached {
			t.Error("expected cached platform to be returned")
		}
		if fakeClient.GetCallCount() != 0 {
			t.Errorf("expected 0 Get calls with cached platform, got %d", fakeClient.GetCallCount())
		}
	})

	t.Run("discovers and caches on first call", func(t *testing.T) {
		fakeClient := &fakes.FakeCtrlClient{}
		fakeClient.GetStub = func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
			u := obj.(*unstructured.Unstructured)
			u.Object = map[string]interface{}{
				"status": map[string]interface{}{
					"platformStatus": map[string]interface{}{
						"type": "AWS",
					},
				},
			}
			return nil
		}
		r := &Reconciler{
			CtrlClient: fakeClient,
			log:        logr.Discard(),
		}

		info, err := r.getOrDiscoverPlatform(context.Background())
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if info.platformType != "AWS" {
			t.Errorf("platformType = %q, want %q", info.platformType, "AWS")
		}
		if r.cachedPlatform == nil {
			t.Error("expected cachedPlatform to be set")
		}

		// Second call should use cache
		info2, err := r.getOrDiscoverPlatform(context.Background())
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if info2 != info {
			t.Error("second call should return same cached pointer")
		}
		if fakeClient.GetCallCount() != 1 {
			t.Errorf("expected 1 Get call total (cached second time), got %d", fakeClient.GetCallCount())
		}
	})

	t.Run("does not cache on error", func(t *testing.T) {
		fakeClient := &fakes.FakeCtrlClient{}
		fakeClient.GetReturns(fmt.Errorf("api unavailable"))
		r := &Reconciler{
			CtrlClient: fakeClient,
			log:        logr.Discard(),
		}

		_, err := r.getOrDiscoverPlatform(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
		if r.cachedPlatform != nil {
			t.Error("cachedPlatform should remain nil on error")
		}
	})
}
