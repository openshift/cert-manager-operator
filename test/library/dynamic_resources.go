//go:build e2e

package library

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/wait"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
)

type DynamicResourceLoader struct {
	KubeClient    kubernetes.Interface
	DynamicClient dynamic.Interface

	context context.Context
	t       *testing.T
}

type doFunc func(t *testing.T, unstructured *unstructured.Unstructured, dynamicResourceInterface dynamic.ResourceInterface)

func NewDynamicResourceLoader(context context.Context, t *testing.T) DynamicResourceLoader {
	k, d := NewClientsConfigForTest(t)
	return DynamicResourceLoader{
		KubeClient:    k,
		DynamicClient: d,
		context:       context,
		t:             t,
	}
}

func (d DynamicResourceLoader) noErrorSkipExists(err error) {
	// Intentionally ignores AlreadyExists for manifest-driven fixture setup. Callers that
	// require specific object data should use library.UpsertSecret/UpsertConfigMap or an
	// ensure* helper instead of relying on CreateFromFile idempotency alone.
	if !k8serrors.IsAlreadyExists(err) {
		require.NoError(d.t, err)
	}
}

func (d DynamicResourceLoader) noErrorSkipNotExisting(err error) {
	if !k8serrors.IsNotFound(err) {
		require.NoError(d.t, err)
	}
}

// isRetriableAPIError reports a transient API or transport failure. Kept in this
// package so manifest deletes can retry without importing the e2e suite.
func isRetriableAPIError(err error) bool {
	if err == nil {
		return false
	}
	if k8serrors.IsTimeout(err) ||
		k8serrors.IsServerTimeout(err) ||
		k8serrors.IsTooManyRequests(err) ||
		k8serrors.IsServiceUnavailable(err) ||
		k8serrors.IsInternalError(err) {
		return true
	}
	msg := strings.ToLower(err.Error())
	for _, needle := range []string{
		"connection reset",
		"connection refused",
		"http2: client connection lost",
		"unexpected eof",
		"tls: handshake timeout",
		"i/o timeout",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// retryRetriable runs fn until it succeeds. Transient API errors are retried.
// Any other error is returned immediately.
func retryRetriable(ctx context.Context, fn func(context.Context) error) error {
	return wait.PollUntilContextTimeout(ctx, 2*time.Second, 3*time.Minute, true, func(pollCtx context.Context) (bool, error) {
		err := fn(pollCtx)
		if err == nil {
			return true, nil
		}
		if isRetriableAPIError(err) {
			return false, nil
		}
		return false, err
	})
}

func (d DynamicResourceLoader) do(do doFunc, assetFunc func(name string) ([]byte, error), filename string, overrideNamespace string) {
	b, err := assetFunc(filename)
	require.NoError(d.t, err)

	decoder := yamlutil.NewYAMLOrJSONDecoder(bytes.NewReader(b), 1024)
	for {
		var rawObj runtime.RawExtension
		err := decoder.Decode(&rawObj)
		if err == io.EOF {
			break
		}
		require.NoError(d.t, err)
		if len(rawObj.Raw) == 0 {
			continue
		}

		obj, gvk, err := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme).Decode(rawObj.Raw, nil, nil)
		require.NoError(d.t, err)

		unstructuredMap, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
		require.NoError(d.t, err)

		unstructuredObj := &unstructured.Unstructured{Object: unstructuredMap}

		var gr []*restmapper.APIGroupResources
		err = retryRetriable(d.context, func(context.Context) error {
			var getErr error
			gr, getErr = restmapper.GetAPIGroupResources(d.KubeClient.Discovery())
			return getErr
		})
		require.NoError(d.t, err)

		mapper := restmapper.NewDiscoveryRESTMapper(gr)
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		require.NoError(d.t, err)

		var dri dynamic.ResourceInterface
		if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
			if overrideNamespace != "" {
				unstructuredObj.SetNamespace(overrideNamespace)
			}
			require.NotEmpty(d.t, unstructuredObj.GetNamespace(), "Namespace can not be empty!")
			dri = d.DynamicClient.Resource(mapping.Resource).Namespace(unstructuredObj.GetNamespace())
		} else {
			dri = d.DynamicClient.Resource(mapping.Resource)
		}

		do(d.t, unstructuredObj, dri)
	}
}

func (d DynamicResourceLoader) DeleteFromFile(assetFunc func(name string) ([]byte, error), filename string, overrideNamespace string) {
	d.t.Logf("Deleting resource %v\n", filename)
	deleteFunc := func(t *testing.T, unstructured *unstructured.Unstructured, dynamicResourceInterface dynamic.ResourceInterface) {
		err := retryRetriable(d.context, func(ctx context.Context) error {
			err := dynamicResourceInterface.Delete(ctx, unstructured.GetName(), metav1.DeleteOptions{})
			if k8serrors.IsNotFound(err) {
				return nil
			}
			return err
		})
		d.noErrorSkipNotExisting(err)
	}

	d.do(deleteFunc, assetFunc, filename, overrideNamespace)
	d.t.Logf("Resource %v deleted\n", filename)
}

func (d DynamicResourceLoader) CreateFromFile(assetFunc func(name string) ([]byte, error), filename string, overrideNamespace string) {
	d.t.Logf("Creating resource %v\n", filename)
	createFunc := func(t *testing.T, unstructured *unstructured.Unstructured, dynamicResourceInterface dynamic.ResourceInterface) {
		_, err := dynamicResourceInterface.Create(context.TODO(), unstructured, metav1.CreateOptions{})
		d.noErrorSkipExists(err)
	}

	d.do(createFunc, assetFunc, filename, overrideNamespace)
	d.t.Logf("Resource %v created\n", filename)
}
