package common

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/conversion"
)

// fakeManager implements manager.Manager for unit testing NewClient.
// Only GetClient is used by NewClient; other methods return nil/zero/no-op.
type fakeManager struct {
	client client.Client
}

func (f *fakeManager) GetClient() client.Client                                { return f.client }
func (f *fakeManager) GetCache() cache.Cache                                   { return nil }
func (f *fakeManager) GetScheme() *runtime.Scheme                              { return nil }
func (f *fakeManager) GetConfig() *rest.Config                                 { return &rest.Config{} }
func (f *fakeManager) GetHTTPClient() *http.Client                             { return &http.Client{} }
func (f *fakeManager) GetFieldIndexer() client.FieldIndexer                    { return nil }
func (f *fakeManager) GetEventRecorderFor(string) record.EventRecorder         { return nil }
func (f *fakeManager) GetEventRecorder(string) events.EventRecorder            { return nil }
func (f *fakeManager) GetRESTMapper() meta.RESTMapper                          { return nil }
func (f *fakeManager) GetAPIReader() client.Reader                             { return f.client }
func (f *fakeManager) Start(context.Context) error                             { return nil }
func (f *fakeManager) Add(manager.Runnable) error                              { return nil }
func (f *fakeManager) Elected() <-chan struct{}                                { ch := make(chan struct{}); close(ch); return ch }
func (f *fakeManager) AddMetricsServerExtraHandler(string, http.Handler) error { return nil }
func (f *fakeManager) AddHealthzCheck(string, healthz.Checker) error           { return nil }
func (f *fakeManager) AddReadyzCheck(string, healthz.Checker) error            { return nil }
func (f *fakeManager) GetWebhookServer() webhook.Server                        { return nil }
func (f *fakeManager) GetLogger() logr.Logger                                  { return logr.Discard() }
func (f *fakeManager) GetControllerOptions() config.Controller                 { return config.Controller{} }
func (f *fakeManager) GetConverterRegistry() conversion.Registry               { return nil }

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
func (noopSubResourceWriter) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
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
func (noopSubResourceClient) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	return nil
}

func (*sentinelClient) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return nil
}
func (*sentinelClient) List(context.Context, client.ObjectList, ...client.ListOption) error {
	return nil
}
func (*sentinelClient) Apply(context.Context, runtime.ApplyConfiguration, ...client.ApplyOption) error {
	return nil
}
func (*sentinelClient) Create(context.Context, client.Object, ...client.CreateOption) error {
	return nil
}
func (*sentinelClient) Delete(context.Context, client.Object, ...client.DeleteOption) error {
	return nil
}
func (*sentinelClient) Update(context.Context, client.Object, ...client.UpdateOption) error {
	return nil
}
func (*sentinelClient) Patch(context.Context, client.Object, client.Patch, ...client.PatchOption) error {
	return nil
}
func (*sentinelClient) DeleteAllOf(context.Context, client.Object, ...client.DeleteAllOfOption) error {
	return nil
}
func (*sentinelClient) Status() client.SubResourceWriter            { return noopSubResourceWriter{} }
func (*sentinelClient) SubResource(string) client.SubResourceClient { return noopSubResourceClient{} }
func (*sentinelClient) Scheme() *runtime.Scheme                     { return nil }
func (*sentinelClient) RESTMapper() meta.RESTMapper                 { return nil }
func (*sentinelClient) GroupVersionKindFor(runtime.Object) (schema.GroupVersionKind, error) {
	return schema.GroupVersionKind{}, nil
}
func (*sentinelClient) IsObjectNamespaced(runtime.Object) (bool, error) { return false, nil }

func TestNewClient(t *testing.T) {
	var cl client.Client = &sentinelClient{}
	mgr := &fakeManager{client: cl}

	got, err := NewClient(mgr)
	require.NoError(t, err)
	require.NotNil(t, got)
	impl, ok := got.(*ctrlClientImpl)
	require.True(t, ok, "NewClient must return *ctrlClientImpl")
	assert.True(t, impl.Client == cl, "wrapped client must be the exact manager client instance")
}

type mockClient struct {
	client.Client

	getFunc    func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	createFunc func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	updateFunc func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	deleteFunc func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	listFunc   func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	patchFunc  func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error

	statusClient *mockStatusClient
}

type mockStatusClient struct {
	updateFunc func(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error
}

func (m *mockStatusClient) Update(ctx context.Context, obj client.Object, opts ...client.SubResourceUpdateOption) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockStatusClient) Create(context.Context, client.Object, client.Object, ...client.SubResourceCreateOption) error {
	return nil
}

func (m *mockStatusClient) Patch(context.Context, client.Object, client.Patch, ...client.SubResourcePatchOption) error {
	return nil
}

func (m *mockStatusClient) Apply(context.Context, runtime.ApplyConfiguration, ...client.SubResourceApplyOption) error {
	return nil
}

func (m *mockClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if m.getFunc != nil {
		return m.getFunc(ctx, key, obj, opts...)
	}
	return nil
}

func (m *mockClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	if m.listFunc != nil {
		return m.listFunc(ctx, list, opts...)
	}
	return nil
}

func (m *mockClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	if m.patchFunc != nil {
		return m.patchFunc(ctx, obj, patch, opts...)
	}
	return nil
}

func (m *mockClient) Status() client.SubResourceWriter {
	if m.statusClient != nil {
		return m.statusClient
	}
	return &mockStatusClient{}
}

func newCtrlClient(mc *mockClient) *ctrlClientImpl {
	return &ctrlClientImpl{Client: mc}
}

func TestClientExists_Found(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return nil
		},
	}
	c := newCtrlClient(mc)

	found, err := c.Exists(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, &corev1.ConfigMap{})
	require.NoError(t, err)
	assert.True(t, found)
}

func TestClientExists_NotFound(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "missing")
		},
	}
	c := newCtrlClient(mc)

	found, err := c.Exists(context.Background(), types.NamespacedName{Name: "missing", Namespace: "default"}, &corev1.ConfigMap{})
	require.NoError(t, err)
	assert.False(t, found)
}

func TestClientExists_Error(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return fmt.Errorf("connection refused")
		},
	}
	c := newCtrlClient(mc)

	found, err := c.Exists(context.Background(), types.NamespacedName{Name: "test", Namespace: "default"}, &corev1.ConfigMap{})
	require.Error(t, err)
	assert.False(t, found)
}

func TestClientGet_Success(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			cm := obj.(*corev1.ConfigMap)
			cm.Name = key.Name
			cm.Namespace = key.Namespace
			cm.Data = map[string]string{"k": "v"}
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{}
	err := c.Get(context.Background(), types.NamespacedName{Name: "myconfig", Namespace: "default"}, cm)
	require.NoError(t, err)
	assert.Equal(t, "myconfig", cm.Name)
	assert.Equal(t, "v", cm.Data["k"])
}

func TestClientGet_NotFound(t *testing.T) {
	mc := &mockClient{
		getFunc: func(_ context.Context, _ client.ObjectKey, _ client.Object, _ ...client.GetOption) error {
			return apierrors.NewNotFound(schema.GroupResource{Resource: "configmaps"}, "missing")
		},
	}
	c := newCtrlClient(mc)

	err := c.Get(context.Background(), types.NamespacedName{Name: "missing", Namespace: "default"}, &corev1.ConfigMap{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get object")
}

func TestClientCreate_Success(t *testing.T) {
	var created bool
	mc := &mockClient{
		createFunc: func(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
			created = true
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "new", Namespace: "default"}}
	err := c.Create(context.Background(), cm)
	require.NoError(t, err)
	assert.True(t, created)
}

func TestClientCreate_AlreadyExists(t *testing.T) {
	mc := &mockClient{
		createFunc: func(_ context.Context, _ client.Object, _ ...client.CreateOption) error {
			return apierrors.NewAlreadyExists(schema.GroupResource{Resource: "configmaps"}, "dup")
		},
	}
	c := newCtrlClient(mc)

	err := c.Create(context.Background(), &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "dup", Namespace: "default"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create object")
}

func TestClientUpdate_Success(t *testing.T) {
	var updated bool
	mc := &mockClient{
		updateFunc: func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
			updated = true
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "upd", Namespace: "default"}}
	err := c.Update(context.Background(), cm)
	require.NoError(t, err)
	assert.True(t, updated)
}

func TestClientDelete_Success(t *testing.T) {
	var deleted bool
	mc := &mockClient{
		deleteFunc: func(_ context.Context, _ client.Object, _ ...client.DeleteOption) error {
			deleted = true
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "del", Namespace: "default"}}
	err := c.Delete(context.Background(), cm)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestClientList_Success(t *testing.T) {
	mc := &mockClient{
		listFunc: func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
			cmList := list.(*corev1.ConfigMapList)
			cmList.Items = []corev1.ConfigMap{
				{ObjectMeta: metav1.ObjectMeta{Name: "cm1"}},
				{ObjectMeta: metav1.ObjectMeta{Name: "cm2"}},
			}
			return nil
		},
	}
	c := newCtrlClient(mc)

	list := &corev1.ConfigMapList{}
	err := c.List(context.Background(), list)
	require.NoError(t, err)
	assert.Len(t, list.Items, 2)
}

func TestClientUpdateWithRetry_Success(t *testing.T) {
	updateCallCount := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			obj.SetResourceVersion("123")
			return nil
		},
		updateFunc: func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
			updateCallCount++
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "retry-cm", Namespace: "default"}}
	err := c.UpdateWithRetry(context.Background(), cm)
	require.NoError(t, err)
	assert.Equal(t, 1, updateCallCount)
	assert.Equal(t, "123", cm.GetResourceVersion())
}

func TestClientUpdateWithRetry_ConflictThenSuccess(t *testing.T) {
	callCount := 0
	mc := &mockClient{
		getFunc: func(_ context.Context, _ client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
			obj.SetResourceVersion(fmt.Sprintf("rv-%d", callCount))
			return nil
		},
		updateFunc: func(_ context.Context, _ client.Object, _ ...client.UpdateOption) error {
			callCount++
			if callCount == 1 {
				return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, "retry-cm", fmt.Errorf("conflict"))
			}
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "retry-cm", Namespace: "default"}}
	err := c.UpdateWithRetry(context.Background(), cm)
	require.NoError(t, err)
	assert.Equal(t, 2, callCount, "update should be called twice: conflict then success")
}

func TestClientPatch_Success(t *testing.T) {
	var patched bool
	mc := &mockClient{
		patchFunc: func(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
			patched = true
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "patch-cm", Namespace: "default"}}
	err := c.Patch(context.Background(), cm, client.MergeFrom(cm))
	require.NoError(t, err)
	assert.True(t, patched)
}

func TestClientPatch_Error(t *testing.T) {
	mc := &mockClient{
		patchFunc: func(_ context.Context, _ client.Object, _ client.Patch, _ ...client.PatchOption) error {
			return fmt.Errorf("patch failed")
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "patch-cm", Namespace: "default"}}
	err := c.Patch(context.Background(), cm, client.MergeFrom(cm))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to patch object")
}

func TestClientStatusUpdate_Success(t *testing.T) {
	var updated bool
	mc := &mockClient{
		statusClient: &mockStatusClient{
			updateFunc: func(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				updated = true
				return nil
			},
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "status-cm", Namespace: "default"}}
	err := c.StatusUpdate(context.Background(), cm)
	require.NoError(t, err)
	assert.True(t, updated)
}

func TestClientStatusUpdate_Error(t *testing.T) {
	mc := &mockClient{
		statusClient: &mockStatusClient{
			updateFunc: func(_ context.Context, _ client.Object, _ ...client.SubResourceUpdateOption) error {
				return fmt.Errorf("status update failed")
			},
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "status-cm", Namespace: "default"}}
	err := c.StatusUpdate(context.Background(), cm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to update status")
}
