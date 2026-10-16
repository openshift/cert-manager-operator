package operator

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/cert-manager-operator/pkg/controller/common"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	"github.com/openshift/cert-manager-operator/pkg/controller/trustmanager"
)

func TestBuildCacheObjectList(t *testing.T) {
	t.Run("single controller enabled (IstioCSR)", func(t *testing.T) {
		config := ControllerConfig{
			EnableIstioCSR:     true,
			EnableTrustManager: false,
		}

		objectList, err := buildCacheObjectList(config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// IstioCSR manages 9 resource types plus 1 CR type.
		// Verify at least the Deployment entry exists with the correct label selector.
		found := false
		for key, byObj := range objectList {
			if _, ok := key.(*appsv1.Deployment); ok {
				found = true
				sel := byObj.Label
				if sel == nil {
					t.Fatal("expected label selector for Deployment, got nil")
				}
				selectorStr := sel.String()
				if selectorStr == "" {
					t.Fatal("expected non-empty label selector for Deployment")
				}
				// Should match only the IstioCSR label value.
				testLabels := map[string]string{common.ManagedResourceLabelKey: istiocsr.RequestEnqueueLabelValue}
				if !sel.Matches(labels.Set(testLabels)) {
					t.Errorf("expected selector to match IstioCSR label value %q, selector: %s",
						istiocsr.RequestEnqueueLabelValue, selectorStr)
				}
				break
			}
		}
		if !found {
			t.Error("expected Deployment entry in cache object list")
		}
	})

	t.Run("multiple controllers enabled (labels merge)", func(t *testing.T) {
		config := ControllerConfig{
			EnableIstioCSR:     true,
			EnableTrustManager: true,
		}

		objectList, err := buildCacheObjectList(config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// Deployment is shared between IstioCSR and TrustManager.
		// The label selector should match both values via the In operator.
		for key, byObj := range objectList {
			if _, ok := key.(*appsv1.Deployment); ok {
				sel := byObj.Label
				if sel == nil {
					t.Fatal("expected label selector for Deployment, got nil")
				}

				istioLabels := map[string]string{common.ManagedResourceLabelKey: istiocsr.RequestEnqueueLabelValue}
				trustLabels := map[string]string{common.ManagedResourceLabelKey: trustmanager.RequestEnqueueLabelValue}

				if !sel.Matches(labels.Set(istioLabels)) {
					t.Errorf("expected merged selector to match IstioCSR label, selector: %s", sel.String())
				}
				if !sel.Matches(labels.Set(trustLabels)) {
					t.Errorf("expected merged selector to match TrustManager label, selector: %s", sel.String())
				}
				break
			}
		}
	})

	t.Run("no controllers enabled", func(t *testing.T) {
		config := ControllerConfig{
			EnableIstioCSR:     false,
			EnableTrustManager: false,
		}

		objectList, err := buildCacheObjectList(config)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(objectList) != 0 {
			t.Errorf("expected empty object list when no controllers enabled, got %d entries", len(objectList))
		}
	})
}

func TestAddControllerCacheConfig(t *testing.T) {
	t.Run("adds new resource type", func(t *testing.T) {
		objectList := make(map[client.Object]cache.ByObject)
		resources := []client.Object{&appsv1.Deployment{}, &corev1.Service{}}

		err := addControllerCacheConfig(objectList, "test-controller", resources)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(objectList) != 2 {
			t.Errorf("expected 2 entries, got %d", len(objectList))
		}

		// Verify the Deployment entry has the correct label selector.
		_, byObj, found := findExistingCacheEntry(objectList, &appsv1.Deployment{})
		if !found {
			t.Fatal("expected Deployment entry in object list")
		}
		testLabels := map[string]string{common.ManagedResourceLabelKey: "test-controller"}
		if !byObj.Label.Matches(labels.Set(testLabels)) {
			t.Errorf("expected selector to match label value %q, selector: %s", "test-controller", byObj.Label.String())
		}
	})

	t.Run("merges existing resource type with In operator", func(t *testing.T) {
		objectList := make(map[client.Object]cache.ByObject)
		resources1 := []client.Object{&appsv1.Deployment{}}
		resources2 := []client.Object{&appsv1.Deployment{}}

		err := addControllerCacheConfig(objectList, "controller-a", resources1)
		if err != nil {
			t.Fatalf("first addControllerCacheConfig failed: %v", err)
		}

		err = addControllerCacheConfig(objectList, "controller-b", resources2)
		if err != nil {
			t.Fatalf("second addControllerCacheConfig failed: %v", err)
		}

		// Should still be 1 entry (merged), not 2.
		if len(objectList) != 1 {
			t.Errorf("expected 1 merged entry, got %d", len(objectList))
		}

		_, byObj, found := findExistingCacheEntry(objectList, &appsv1.Deployment{})
		if !found {
			t.Fatal("expected Deployment entry in object list")
		}

		// Selector should match both label values.
		labelsA := map[string]string{common.ManagedResourceLabelKey: "controller-a"}
		labelsB := map[string]string{common.ManagedResourceLabelKey: "controller-b"}
		if !byObj.Label.Matches(labels.Set(labelsA)) {
			t.Errorf("expected merged selector to match controller-a, selector: %s", byObj.Label.String())
		}
		if !byObj.Label.Matches(labels.Set(labelsB)) {
			t.Errorf("expected merged selector to match controller-b, selector: %s", byObj.Label.String())
		}

		// Should NOT match a label value that was never added.
		labelsC := map[string]string{common.ManagedResourceLabelKey: "controller-c"}
		if byObj.Label.Matches(labels.Set(labelsC)) {
			t.Errorf("expected merged selector NOT to match controller-c, selector: %s", byObj.Label.String())
		}
	})
}

func TestFindExistingCacheEntry(t *testing.T) {
	t.Run("returns entry when type found", func(t *testing.T) {
		objectList := map[client.Object]cache.ByObject{
			&appsv1.Deployment{}:  {},
			&corev1.Service{}:     {},
			&rbacv1.ClusterRole{}: {},
		}

		key, _, found := findExistingCacheEntry(objectList, &appsv1.Deployment{})
		if !found {
			t.Fatal("expected to find Deployment entry")
		}
		if _, ok := key.(*appsv1.Deployment); !ok {
			t.Errorf("expected key to be *appsv1.Deployment, got %T", key)
		}
	})

	t.Run("returns not-found when type absent", func(t *testing.T) {
		objectList := map[client.Object]cache.ByObject{
			&appsv1.Deployment{}: {},
			&corev1.Service{}:    {},
		}

		_, _, found := findExistingCacheEntry(objectList, &rbacv1.ClusterRole{})
		if found {
			t.Error("expected not-found for ClusterRole, but it was found")
		}
	})

	t.Run("distinguishes different pointer types of same kind", func(t *testing.T) {
		objectList := map[client.Object]cache.ByObject{
			&rbacv1.ClusterRole{}: {},
		}

		_, _, found := findExistingCacheEntry(objectList, &rbacv1.ClusterRoleBinding{})
		if found {
			t.Error("expected not-found for ClusterRoleBinding when only ClusterRole exists")
		}
	})
}
