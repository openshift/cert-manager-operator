//go:build e2e

package library

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	fakediscovery "k8s.io/client-go/discovery/fake"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

const (
	testNamespace     = "istio-csr-test"
	testConfigMapName = "ca-bundle"
	testConfigMapYAML = `apiVersion: v1
kind: ConfigMap
metadata:
  name: ca-bundle
data:
  ca.crt: cert
`
)

var configMapGVR = schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}

// etcdTimeoutError is what the apiserver returns when etcd times out: a 500
// with no StatusReason, which IsInternalError treats as internal and retriable.
func etcdTimeoutError() error {
	return &k8serrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Message: "etcdserver: request timed out",
		Code:    http.StatusInternalServerError,
	}}
}

func TestIsRetriableAPIErrorEtcdTimeout(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "etcd request timed out as a plain 500", err: etcdTimeoutError(), want: true},
		{name: "already exists", err: k8serrors.NewAlreadyExists(configMapGVR.GroupResource(), testConfigMapName), want: false},
		{name: "forbidden", err: k8serrors.NewForbidden(configMapGVR.GroupResource(), testConfigMapName, fmt.Errorf("denied")), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetriableAPIError(tt.err); got != tt.want {
				t.Fatalf("isRetriableAPIError() = %v, want %v (err=%v)", got, tt.want, tt.err)
			}
		})
	}
}

// configMapLoader returns a loader that discovers core/v1 ConfigMaps and whose
// first n ConfigMap creates fail with err. existing objects are pre-seeded, as
// if an earlier create had already succeeded server-side.
func configMapLoader(t *testing.T, n int, err error, existing ...runtime.Object) (DynamicResourceLoader, *dynamicfake.FakeDynamicClient, *int) {
	t.Helper()

	kube := fake.NewClientset()
	kube.Discovery().(*fakediscovery.FakeDiscovery).Resources = []*metav1.APIResourceList{{
		GroupVersion: "v1",
		APIResources: []metav1.APIResource{{Name: configMapGVR.Resource, Namespaced: true, Kind: "ConfigMap", Verbs: metav1.Verbs{"create", "get"}}},
	}}

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatalf("add corev1 to scheme: %v", err)
	}
	dyn := dynamicfake.NewSimpleDynamicClient(scheme, existing...)
	creates := 0
	dyn.PrependReactor("create", configMapGVR.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		creates++
		if creates <= n {
			return true, nil, err
		}
		return false, nil, nil
	})

	return DynamicResourceLoader{KubeClient: kube, DynamicClient: dyn, context: context.Background(), t: t}, dyn, &creates
}

func configMapAsset(string) ([]byte, error) { return []byte(testConfigMapYAML), nil }

func TestCreateFromFileRetriesTransientCreateError(t *testing.T) {
	loader, dyn, creates := configMapLoader(t, 1, etcdTimeoutError())

	loader.CreateFromFile(configMapAsset, "configmap.yaml", testNamespace)

	if *creates != 2 {
		t.Fatalf("Create calls = %d, want 2: the etcd timeout must be retried once", *creates)
	}
	if _, err := dyn.Resource(configMapGVR).Namespace(testNamespace).Get(context.Background(), testConfigMapName, metav1.GetOptions{}); err != nil {
		t.Fatalf("ConfigMap not created after retry: %v", err)
	}
}

// TestCreateFromFileIgnoresAlreadyExistsAfterRetry covers a create that timed
// out but succeeded server-side: the retry gets AlreadyExists, which must stop
// the retry and not fail the spec.
func TestCreateFromFileIgnoresAlreadyExistsAfterRetry(t *testing.T) {
	existing := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: testConfigMapName, Namespace: testNamespace}}
	loader, _, creates := configMapLoader(t, 1, etcdTimeoutError(), existing)

	loader.CreateFromFile(configMapAsset, "configmap.yaml", testNamespace)

	if *creates != 2 {
		t.Fatalf("Create calls = %d, want 2: one timeout, then one AlreadyExists", *creates)
	}
}
