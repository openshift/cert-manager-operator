package common

import (
	"context"
	"errors"
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
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
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

// finalizerClient supplies just the reads and writes used by UpdateFinalizer.
type finalizerClient struct {
	CtrlClient
	get    func(context.Context, client.ObjectKey, client.Object) error
	update func(context.Context, client.Object, ...client.UpdateOption) error
}

func (c *finalizerClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	return c.get(ctx, key, obj)
}

func (c *finalizerClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.update(ctx, obj, opts...)
}

func TestUpdateFinalizer(t *testing.T) {
	const finalizer = "example.test/controller"
	for _, tc := range []struct {
		name           string
		add            bool
		finalizers     []string
		wantFinalizers []string
	}{
		{name: "add", add: true, finalizers: []string{"other"}, wantFinalizers: []string{"other", finalizer}},
		{name: "remove", finalizers: []string{finalizer, "other"}, wantFinalizers: []string{"other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stored := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "test-ns", UID: "original", ResourceVersion: "1", Finalizers: tc.finalizers}}
			initial := stored.DeepCopy()
			reads, writes := 0, 0
			c := &finalizerClient{
				get: func(_ context.Context, key client.ObjectKey, obj client.Object) error {
					reads++
					assert.Equal(t, client.ObjectKeyFromObject(stored), key)
					stored.DeepCopyInto(obj.(*corev1.ConfigMap))
					return nil
				},
				update: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					writes++
					obj.SetResourceVersion("2")
					obj.(*corev1.ConfigMap).DeepCopyInto(stored)
					return nil
				},
			}

			updated, err := UpdateFinalizer(t.Context(), c, initial, corev1.Resource("configmaps"), finalizer, tc.add)
			require.NoError(t, err)
			assert.Equal(t, tc.wantFinalizers, stored.Finalizers)
			assert.Equal(t, stored, updated)
			assert.Equal(t, tc.finalizers, initial.Finalizers, "the caller's snapshot is not mutated")
			assert.Equal(t, "1", initial.ResourceVersion)

			// Repeating an already completed change requires no further reads or writes.
			again, err := UpdateFinalizer(t.Context(), c, updated, corev1.Resource("configmaps"), finalizer, tc.add)
			require.NoError(t, err)
			assert.Same(t, updated, again)
			assert.Equal(t, 1, reads)
			assert.Equal(t, 1, writes)
		})
	}
}

func TestUpdateFinalizerRetriesConcurrentChanges(t *testing.T) {
	const finalizer = "example.test/controller"
	for _, add := range []bool{true, false} {
		t.Run(map[bool]string{true: "add", false: "remove"}[add], func(t *testing.T) {
			stored := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "original", ResourceVersion: "1"}}
			if !add {
				stored.Finalizers = []string{finalizer}
			}
			initial := stored.DeepCopy()
			writes := 0
			c := &finalizerClient{
				get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					stored.DeepCopyInto(obj.(*corev1.ConfigMap))
					return nil
				},
				update: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					writes++
					if writes == 1 {
						// Another writer changes both metadata and content before our update.
						stored.Finalizers = append(stored.Finalizers, "other")
						stored.Labels = map[string]string{"owner": "other"}
						stored.Data = map[string]string{"key": "concurrent value"}
						stored.ResourceVersion = "2"
						return apierrors.NewConflict(corev1.Resource("configmaps"), stored.Name, errors.New("concurrent edit"))
					}
					assert.Equal(t, "2", obj.GetResourceVersion())
					obj.(*corev1.ConfigMap).DeepCopyInto(stored)
					return nil
				},
			}

			updated, err := UpdateFinalizer(t.Context(), c, initial, corev1.Resource("configmaps"), finalizer, add)
			require.NoError(t, err)
			assert.Equal(t, 2, writes)
			assert.Equal(t, stored, updated)
			assert.Contains(t, updated.Finalizers, "other")
			assert.Equal(t, map[string]string{"owner": "other"}, updated.Labels)
			assert.Equal(t, map[string]string{"key": "concurrent value"}, updated.Data)
			if add {
				assert.Contains(t, updated.Finalizers, finalizer)
			} else {
				assert.NotContains(t, updated.Finalizers, finalizer)
			}
			assert.Empty(t, initial.Data)
			assert.Equal(t, "1", initial.ResourceVersion)
		})
	}
}

func TestUpdateFinalizerAlreadyChangedByAnotherWriter(t *testing.T) {
	const finalizer = "example.test/controller"
	for _, add := range []bool{true, false} {
		t.Run(map[bool]string{true: "add", false: "remove"}[add], func(t *testing.T) {
			initial := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "original"}}
			current := initial.DeepCopy()
			if add {
				current.Finalizers = []string{finalizer}
			} else {
				initial.Finalizers = []string{finalizer}
			}
			current.Data = map[string]string{"key": "concurrent value"}
			c := &finalizerClient{
				get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					current.DeepCopyInto(obj.(*corev1.ConfigMap))
					return nil
				},
				update: func(context.Context, client.Object, ...client.UpdateOption) error {
					t.Fatal("the finalizer is already in the requested state")
					return nil
				},
			}

			updated, err := UpdateFinalizer(t.Context(), c, initial, corev1.Resource("configmaps"), finalizer, add)
			require.NoError(t, err)
			assert.Equal(t, current, updated, "return concurrent changes even when no update is needed")
		})
	}
}

func TestUpdateFinalizerDeletionAndReplacement(t *testing.T) {
	const finalizer = "example.test/controller"
	for _, tc := range []struct {
		name         string
		add          bool
		deleting     bool
		replaced     bool
		wantConflict bool
		wantWrites   int
	}{
		{name: "skip adding after deletion starts", add: true, deleting: true},
		{name: "allow removal during deletion", deleting: true, wantWrites: 1},
		{name: "do not add to a replacement", add: true, replaced: true, wantConflict: true},
		{name: "do not remove from a replacement", replaced: true, wantConflict: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "original"}}
			if !tc.add {
				initial.Finalizers = []string{finalizer, "other"}
			}
			current := initial.DeepCopy()
			if tc.deleting {
				now := metav1.Now()
				current.DeletionTimestamp = &now
			}
			if tc.replaced {
				current.UID = "replacement"
			}
			writes := 0
			c := &finalizerClient{
				get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					current.DeepCopyInto(obj.(*corev1.ConfigMap))
					return nil
				},
				update: func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
					writes++
					assert.NotContains(t, obj.GetFinalizers(), finalizer)
					assert.Contains(t, obj.GetFinalizers(), "other")
					return nil
				},
			}

			updated, err := UpdateFinalizer(t.Context(), c, initial, corev1.Resource("configmaps"), finalizer, tc.add)
			if tc.wantConflict {
				require.True(t, apierrors.IsConflict(err), "expected conflict, got %v", err)
				assert.Same(t, initial, updated)
			} else {
				require.NoError(t, err)
				assert.Equal(t, current.DeletionTimestamp, updated.DeletionTimestamp)
			}
			assert.Equal(t, tc.wantWrites, writes)
			assert.Nil(t, initial.DeletionTimestamp)
			assert.Equal(t, "original", string(initial.UID))
		})
	}
}

func TestUpdateFinalizerErrors(t *testing.T) {
	readErr := apierrors.NewTimeoutError("read failed", 1)
	writeErr := errors.New("update failed")
	conflictErr := apierrors.NewConflict(corev1.Resource("configmaps"), "test", errors.New("concurrent edit"))
	for _, tc := range []struct {
		name       string
		readErr    error
		writeErr   error
		wantErr    error
		wantWrites int
	}{
		{name: "read failure", readErr: readErr, wantErr: readErr},
		{name: "object disappeared", readErr: apierrors.NewNotFound(corev1.Resource("configmaps"), "test")},
		{name: "update failure", writeErr: writeErr, wantErr: writeErr, wantWrites: 1},
		{name: "conflict retries exhausted", writeErr: conflictErr, wantErr: conflictErr, wantWrites: retry.DefaultRetry.Steps},
	} {
		t.Run(tc.name, func(t *testing.T) {
			initial := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "test", UID: "original"}}
			writes := 0
			c := &finalizerClient{
				get: func(_ context.Context, _ client.ObjectKey, obj client.Object) error {
					initial.DeepCopyInto(obj.(*corev1.ConfigMap))
					return tc.readErr
				},
				update: func(context.Context, client.Object, ...client.UpdateOption) error {
					writes++
					return tc.writeErr
				},
			}

			updated, err := UpdateFinalizer(t.Context(), c, initial, corev1.Resource("configmaps"), "example.test/controller", true)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else {
				require.True(t, apierrors.IsNotFound(err), "expected NotFound, got %v", err)
			}
			assert.Equal(t, tc.wantWrites, writes)
			assert.Same(t, initial, updated)
			assert.Empty(t, initial.Finalizers)
		})
	}
}

type mockClient struct {
	client.Client

	getFunc    func(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error
	createFunc func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error
	updateFunc func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error
	deleteFunc func(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error
	listFunc   func(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error
	patchFunc  func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error
	applyFunc  func(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error
	gvkFunc    func(obj runtime.Object) (schema.GroupVersionKind, error)

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

func (m *mockClient) Apply(ctx context.Context, obj runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
	if m.applyFunc != nil {
		return m.applyFunc(ctx, obj, opts...)
	}
	return nil
}

func (m *mockClient) GroupVersionKindFor(obj runtime.Object) (schema.GroupVersionKind, error) {
	if m.gvkFunc != nil {
		return m.gvkFunc(obj)
	}
	return apiutil.GVKForObject(obj, clientgoscheme.Scheme)
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

func TestClientApply_Success(t *testing.T) {
	mc := &mockClient{
		applyFunc: func(_ context.Context, ac runtime.ApplyConfiguration, opts ...client.ApplyOption) error {
			obj, ok := ac.(interface {
				runtime.Object
				metav1.Object
			})
			require.True(t, ok, "apply configuration should wrap an unstructured object")
			assert.Equal(t, schema.GroupVersionKind{Version: "v1", Kind: "ConfigMap"}, obj.GetObjectKind().GroupVersionKind())
			assert.Equal(t, "apply-cm", obj.GetName())

			ao := (&client.ApplyOptions{}).ApplyOptions(opts)
			assert.Equal(t, "test-manager", ao.FieldManager)

			// Simulate the server response, which must be copied back into the typed object.
			obj.SetResourceVersion("42")
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "apply-cm", Namespace: "default"}}
	err := c.Apply(context.Background(), cm, client.FieldOwner("test-manager"))
	require.NoError(t, err)
	assert.Equal(t, "42", cm.ResourceVersion)
}

func TestClientApply_GVKError(t *testing.T) {
	var applied bool
	mc := &mockClient{
		gvkFunc: func(runtime.Object) (schema.GroupVersionKind, error) {
			return schema.GroupVersionKind{}, fmt.Errorf("not registered")
		},
		applyFunc: func(context.Context, runtime.ApplyConfiguration, ...client.ApplyOption) error {
			applied = true
			return nil
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "apply-cm", Namespace: "default"}}
	err := c.Apply(context.Background(), cm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get GroupVersionKind")
	assert.False(t, applied, "apply should not be sent without a GroupVersionKind")
}

func TestClientApply_Error(t *testing.T) {
	mc := &mockClient{
		applyFunc: func(context.Context, runtime.ApplyConfiguration, ...client.ApplyOption) error {
			return fmt.Errorf("apply failed")
		},
	}
	c := newCtrlClient(mc)

	cm := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "apply-cm", Namespace: "default"}}
	err := c.Apply(context.Background(), cm)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to apply object")
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
