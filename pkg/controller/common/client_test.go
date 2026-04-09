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
	"k8s.io/apimachinery/pkg/runtime/schema"
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

// sentinelClient is a non-nil client.Client stub so NewClient tests can assert manager wiring
// (pointer identity) without controller-runtime's fake client (not vendored).
type sentinelClient struct{}

type noopSubResourceWriter struct{}

func (noopSubResourceWriter) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	return nil
}
func (noopSubResourceWriter) Update(context.Context, client.Object, ...client.SubResourceUpdateOption) error {
	return nil
}
func (noopSubResourceWriter) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return nil
}

type noopSubResourceClient struct{}

func (noopSubResourceClient) Get(context.Context, client.Object, client.Object, ...client.SubResourceGetOption) error {
	return nil
}
func (noopSubResourceClient) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	return nil
}
func (noopSubResourceClient) Update(context.Context, client.Object, ...client.SubResourceUpdateOption) error {
	return nil
}
func (noopSubResourceClient) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return nil
}

func (*sentinelClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return nil
}
func (*sentinelClient) List(context.Context, client.ObjectList, ...client.ListOption) error { return nil }
func (*sentinelClient) Apply(context.Context, runtime.ApplyConfiguration, ...client.ApplyOption) error {
	return nil
}
func (*sentinelClient) Create(context.Context, client.Object, ...client.CreateOption) error { return nil }
func (*sentinelClient) Delete(context.Context, client.Object, ...client.DeleteOption) error { return nil }
func (*sentinelClient) Update(context.Context, client.Object, ...client.UpdateOption) error { return nil }
func (*sentinelClient) Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error {
	return nil
}
func (*sentinelClient) DeleteAllOf(context.Context, client.Object, ...client.DeleteAllOfOption) error {
	return nil
}
func (*sentinelClient) Status() client.SubResourceWriter                            { return noopSubResourceWriter{} }
func (*sentinelClient) SubResource(string) client.SubResourceClient                 { return noopSubResourceClient{} }
func (*sentinelClient) Scheme() *runtime.Scheme                                     { return nil }
func (*sentinelClient) RESTMapper() meta.RESTMapper                                 { return nil }
func (*sentinelClient) GroupVersionKindFor(runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}
func (*sentinelClient) IsObjectNamespaced(runtime.Object) (bool, error) { return false, nil }

// TestNewClient verifies NewClient returns a CtrlClient that wraps the same client instance
// as manager.GetClient() (cache-consistent wiring).
func TestNewClient(t *testing.T) {
	var cl client.Client = &sentinelClient{}
	mgr := &fakeManager{client: cl}

	got, err := NewClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, got)
	var _ CtrlClient = got
	impl, ok := got.(*ctrlClientImpl)
	require.True(t, ok, "NewClient must return *ctrlClientImpl")
	assert.True(t, impl.Client == cl, "wrapped client must be the exact manager client instance")
}
