package certmanager

import (
	"context"
	"testing"

	"github.com/openshift/library-go/pkg/controller/factory"
)

type stubController struct{}

func (s *stubController) Run(ctx context.Context, workers int)                          {}
func (s *stubController) Sync(ctx context.Context, syncContext factory.SyncContext) error { return nil }
func (s *stubController) Name() string                                                   { return "stub" }

func TestToArrayConsoleControllerInclusion(t *testing.T) {
	stub := &stubController{}

	tests := []struct {
		name           string
		includeConsole bool
		wantCount      int
	}{
		{name: "with console controller", includeConsole: true, wantCount: 9},
		{name: "without console controller", includeConsole: false, wantCount: 8},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set := &CertManagerControllerSet{
				certManagerControllerStaticResourcesController:    stub,
				certManagerControllerDeploymentController:         stub,
				certManagerWebhookStaticResourcesController:       stub,
				certManagerWebhookDeploymentController:            stub,
				certManagerCAInjectorStaticResourcesController:    stub,
				certManagerCAInjectorDeploymentController:         stub,
				certManagerNetworkPolicyStaticResourcesController: stub,
				certManagerNetworkPolicyUserDefinedController:     stub,
			}
			if tt.includeConsole {
				set.consoleResourcesController = stub
			}

			controllers := set.ToArray()
			if len(controllers) != tt.wantCount {
				t.Errorf("ToArray() returned %d controllers, want %d", len(controllers), tt.wantCount)
			}
			for i, c := range controllers {
				if c == nil {
					t.Errorf("ToArray()[%d] is nil", i)
				}
			}
		})
	}
}
