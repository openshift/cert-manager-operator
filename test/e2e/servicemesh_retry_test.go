//go:build e2e
// +build e2e

package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	dynamicfake "k8s.io/client-go/dynamic/fake"
	k8stesting "k8s.io/client-go/testing"

	"github.com/openshift/cert-manager-operator/test/library"
)

// installedClusterExtension is the OSSM ClusterExtension with Installed=True.
func installedClusterExtension() *unstructured.Unstructured {
	ce := &unstructured.Unstructured{}
	ce.SetAPIVersion(clusterExtensionGVR.GroupVersion().String())
	ce.SetKind("ClusterExtension")
	ce.SetName(ossmClusterExtensionName)
	_ = unstructured.SetNestedSlice(ce.Object, []any{
		map[string]any{"type": "Installed", "status": "True"},
	}, "status", "conditions")
	return ce
}

// clusterExtensionLoader returns a loader whose ClusterExtension GETs fail with
// err for the first n calls and then return an installed ClusterExtension.
func clusterExtensionLoader(n int, err error) library.DynamicResourceLoader {
	dyn := dynamicfake.NewSimpleDynamicClient(runtime.NewScheme(), installedClusterExtension())
	calls := 0
	dyn.PrependReactor("get", clusterExtensionGVR.Resource, func(k8stesting.Action) (bool, runtime.Object, error) {
		calls++
		if calls <= n {
			return true, nil, err
		}
		return false, nil, nil
	})
	return library.DynamicResourceLoader{DynamicClient: dyn}
}

func TestWaitForClusterExtensionReadyRetriesTransientError(t *testing.T) {
	// One retry costs a slowPollInterval, so bound the wait well above that.
	ctx, cancel := context.WithTimeout(context.Background(), slowPollInterval+30*time.Second)
	defer cancel()

	if err := waitForClusterExtensionReady(ctx, clusterExtensionLoader(1, apiServerTimeoutError())); err != nil {
		t.Fatalf("waitForClusterExtensionReady() = %v, want nil: a 504 must be retried, not returned (OSM-SMOKE-TC-001)", err)
	}
}

func TestWaitForClusterExtensionReadyFailsOnRealError(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	forbidden := apierrors.NewForbidden(clusterExtensionGVR.GroupResource(), ossmClusterExtensionName, fmt.Errorf("denied"))
	err := waitForClusterExtensionReady(ctx, clusterExtensionLoader(1, forbidden))
	if !apierrors.IsForbidden(err) {
		t.Fatalf("waitForClusterExtensionReady() = %v, want the Forbidden error returned without retrying", err)
	}
}
