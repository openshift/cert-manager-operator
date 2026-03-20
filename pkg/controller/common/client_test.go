package common

import (
	"context"
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// fakeManager implements manager.Manager for unit testing NewClient.
// Only GetClient is used by NewClient; other methods return nil/zero/no-op.
type fakeManager struct {
	client client.Client
}

func (f *fakeManager) GetClient() client.Client              { return f.client }
func (f *fakeManager) GetCache() cache.Cache                  { return nil }
func (f *fakeManager) GetScheme() *runtime.Scheme             { return nil }
func (f *fakeManager) GetConfig() *rest.Config                { return &rest.Config{} }
func (f *fakeManager) GetHTTPClient() *http.Client            { return &http.Client{} }
func (f *fakeManager) GetFieldIndexer() client.FieldIndexer   { return nil }
func (f *fakeManager) GetEventRecorderFor(string) record.EventRecorder { return nil }
func (f *fakeManager) GetRESTMapper() meta.RESTMapper         { return nil }
func (f *fakeManager) GetAPIReader() client.Reader            { return f.client }
func (f *fakeManager) Start(context.Context) error           { return nil }
func (f *fakeManager) Add(manager.Runnable) error             { return nil }
func (f *fakeManager) Elected() <-chan struct{}               { ch := make(chan struct{}); close(ch); return ch }
func (f *fakeManager) AddMetricsServerExtraHandler(string, http.Handler) error { return nil }
func (f *fakeManager) AddHealthzCheck(string, healthz.Checker) error { return nil }
func (f *fakeManager) AddReadyzCheck(string, healthz.Checker) error   { return nil }
func (f *fakeManager) GetWebhookServer() webhook.Server        { return nil }
func (f *fakeManager) GetLogger() logr.Logger                 { return logr.Discard() }
func (f *fakeManager) GetControllerOptions() config.Controller { return config.Controller{} }

// TestNewClient provides table-driven tests for NewClient(m manager.Manager) (CtrlClient, error).
// Uses a nil client to avoid depending on controller-runtime/pkg/client/fake (not in vendor).
func TestNewClient(t *testing.T) {
	var cl client.Client = nil
	mgr := &fakeManager{client: cl}

	tests := []struct {
		name        string
		m           manager.Manager
		expectPanic bool
	}{
		{
			name:        "happy path - valid manager returns CtrlClient",
			m:           mgr,
			expectPanic: false,
		},
		{
			name:        "nil manager - GetClient panics",
			m:           nil,
			expectPanic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.expectPanic {
				defer func() {
					r := recover()
					if r == nil {
						t.Fatal("expected panic but got none")
					}
					t.Logf("NewClient(nil) panicked as expected: %v", r)
				}()
			}
			got, err := NewClient(tt.m)
			if !tt.expectPanic {
				require.NoError(t, err)
				require.NotNil(t, got)
				var _ CtrlClient = got
			}
		})
	}
}

// TestNewClient_GetClientReturnsSameClient verifies the returned client wraps the manager's client.
func TestNewClient_GetClientReturnsSameClient(t *testing.T) {
	var cl client.Client = nil
	mgr := &fakeManager{client: cl}
	ctrlClient, err := NewClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, ctrlClient)
	impl, ok := ctrlClient.(*ctrlClientImpl)
	require.True(t, ok, "NewClient must return *ctrlClientImpl")
	assert.Equal(t, cl, impl.Client, "client must be the manager's client")
}
