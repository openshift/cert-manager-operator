package common

import (
	"context"
	"errors"
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
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/events"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
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
