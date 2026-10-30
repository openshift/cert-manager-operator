//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestIsRetriableAPIError(t *testing.T) {
	timeout504 := apiServerTimeoutError()
	notFound := apierrors.NewNotFound(schema.GroupResource{Group: "apps", Resource: "deployments"}, "missing")
	forbidden := apierrors.NewForbidden(schema.GroupResource{Group: "apps", Resource: "deployments"}, "x", fmt.Errorf("denied"))

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "kube-apiserver 504 Timeout", err: timeout504, want: true},
		{name: "server timeout", err: apierrors.NewServerTimeout(schema.GroupResource{Resource: "deployments"}, "get", 1), want: true},
		{name: "too many requests", err: apierrors.NewTooManyRequests("slow down", 1), want: true},
		{name: "service unavailable", err: apierrors.NewServiceUnavailable("unavailable"), want: true},
		{name: "internal error", err: apierrors.NewInternalError(fmt.Errorf("boom")), want: true},
		{name: "connection reset", err: fmt.Errorf("read: connection reset by peer"), want: true},
		{name: "not found is not retriable here", err: notFound, want: false},
		{name: "forbidden", err: forbidden, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRetriableAPIError(tt.err); got != tt.want {
				t.Fatalf("isRetriableAPIError() = %v, want %v (err=%v)", got, tt.want, tt.err)
			}
		})
	}
}

// apiServerTimeoutError builds the 504 a kube-apiserver returns when a request
// outlives its deadline. This is the error that failed OSM-SMOKE-TC-001.
func apiServerTimeoutError() error {
	return &apierrors.StatusError{ErrStatus: metav1.Status{
		Status:  metav1.StatusFailure,
		Message: "the server was unable to return a response in the time allotted, but may still be processing the request",
		Reason:  metav1.StatusReasonTimeout,
		Code:    http.StatusGatewayTimeout,
	}}
}

// failFirstConfigMapGets makes the first n ConfigMap GETs fail with err, then
// falls through to the fake's object tracker.
func failFirstConfigMapGets(cs *fake.Clientset, n int, err error) {
	calls := 0
	cs.PrependReactor("get", "configmaps", func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls <= n {
			return true, nil, err
		}
		return false, nil, nil
	})
}

func TestPollTillConfigMapAvailableRetriesTransientError(t *testing.T) {
	const namespace = "osm-member"

	cs := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-ca-root-cert", Namespace: namespace},
	})
	failFirstConfigMapGets(cs, 1, apiServerTimeoutError())

	if err := pollTillConfigMapAvailable(context.Background(), cs, namespace, "istio-ca-root-cert"); err != nil {
		t.Fatalf("pollTillConfigMapAvailable() = %v, want nil: a 504 must be retried, not returned", err)
	}
}

func TestPollTillConfigMapRemainsIgnoresTransientError(t *testing.T) {
	const namespace = "osm-non-member"

	cs := fake.NewClientset()
	failFirstConfigMapGets(cs, 1, apiServerTimeoutError())

	if err := pollTillConfigMapRemains(context.Background(), cs, namespace, "istio-ca-root-cert", 3*time.Second); err != nil {
		t.Fatalf("pollTillConfigMapRemains() = %v, want nil: a 504 says nothing about whether the ConfigMap exists", err)
	}
}

func TestPollTillConfigMapRemainsFailsWhenAbsenceNeverObserved(t *testing.T) {
	const namespace = "osm-non-member"

	cs := fake.NewClientset()
	failFirstConfigMapGets(cs, 100, apiServerTimeoutError())

	if err := pollTillConfigMapRemains(context.Background(), cs, namespace, "istio-ca-root-cert", 3*time.Second); err == nil {
		t.Fatal("pollTillConfigMapRemains() = nil, want an error: no GET ever confirmed the ConfigMap was absent")
	}
}

func TestPollTillConfigMapRemainsStillFailsOnRealError(t *testing.T) {
	const namespace = "osm-non-member"

	cs := fake.NewClientset()
	failFirstConfigMapGets(cs, 1, apierrors.NewForbidden(schema.GroupResource{Resource: "configmaps"}, "istio-ca-root-cert", fmt.Errorf("denied")))

	if err := pollTillConfigMapRemains(context.Background(), cs, namespace, "istio-ca-root-cert", 3*time.Second); err == nil {
		t.Fatal("pollTillConfigMapRemains() = nil, want an error: a non-retriable error must not be swallowed")
	}
}

func TestGetConfigMapRetryingRetriesTransientError(t *testing.T) {
	const namespace = "osm-member"

	cs := fake.NewClientset(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "istio-ca-root-cert", Namespace: namespace},
		Data:       map[string]string{"root-cert.pem": "cert"},
	})
	failFirstConfigMapGets(cs, 1, apiServerTimeoutError())

	configMap, err := getConfigMapRetrying(context.Background(), cs, namespace, "istio-ca-root-cert")
	if err != nil {
		t.Fatalf("getConfigMapRetrying() error = %v, want nil", err)
	}
	if configMap.Data["root-cert.pem"] != "cert" {
		t.Fatalf("getConfigMapRetrying() = %v, want the ConfigMap carrying root-cert.pem", configMap)
	}
}
