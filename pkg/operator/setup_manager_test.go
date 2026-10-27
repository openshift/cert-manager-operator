package operator

import (
	"reflect"
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

// managedLabel returns the managed-resource label set for the given controller value.
func managedLabel(value string) labels.Set {
	return labels.Set{common.ManagedResourceLabelKey: value}
}

func TestBuildCacheObjectList(t *testing.T) {
	tests := []struct {
		name   string
		config ControllerConfig
		// wantDeploymentMatches lists the label values the Deployment entry's
		// selector must match. Empty means the object list must be empty.
		wantDeploymentMatches []string
	}{
		{
			name:                  "single controller enabled (IstioCSR)",
			config:                ControllerConfig{EnableIstioCSR: true},
			wantDeploymentMatches: []string{istiocsr.RequestEnqueueLabelValue},
		},
		{
			name:                  "multiple controllers enabled (labels merge)",
			config:                ControllerConfig{EnableIstioCSR: true, EnableTrustManager: true},
			wantDeploymentMatches: []string{istiocsr.RequestEnqueueLabelValue, trustmanager.RequestEnqueueLabelValue},
		},
		{
			name:   "no controllers enabled",
			config: ControllerConfig{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectList, err := buildCacheObjectList(tt.config)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(tt.wantDeploymentMatches) == 0 {
				if len(objectList) != 0 {
					t.Errorf("expected empty object list when no controllers enabled, got %d entries", len(objectList))
				}
				return
			}

			_, byObj, found := findExistingCacheEntry(objectList, &appsv1.Deployment{})
			if !found {
				t.Fatal("expected Deployment entry in cache object list")
			}
			if byObj.Label == nil || byObj.Label.String() == "" {
				t.Fatal("expected non-empty label selector for Deployment")
			}
			for _, value := range tt.wantDeploymentMatches {
				if !byObj.Label.Matches(managedLabel(value)) {
					t.Errorf("expected selector to match label value %q, selector: %s", value, byObj.Label.String())
				}
			}
		})
	}
}

func TestAddControllerCacheConfig(t *testing.T) {
	type addCall struct {
		controller string
		resources  []client.Object
	}
	tests := []struct {
		name         string
		calls        []addCall
		wantEntries  int
		wantMatch    []string
		wantNotMatch []string
	}{
		{
			name: "adds new resource type",
			calls: []addCall{
				{controller: "test-controller", resources: []client.Object{&appsv1.Deployment{}, &corev1.Service{}}},
			},
			wantEntries: 2,
			wantMatch:   []string{"test-controller"},
		},
		{
			name: "merges existing resource type with In operator",
			calls: []addCall{
				{controller: "controller-a", resources: []client.Object{&appsv1.Deployment{}}},
				{controller: "controller-b", resources: []client.Object{&appsv1.Deployment{}}},
			},
			wantEntries:  1,
			wantMatch:    []string{"controller-a", "controller-b"},
			wantNotMatch: []string{"controller-c"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objectList := make(map[client.Object]cache.ByObject)
			for _, c := range tt.calls {
				if err := addControllerCacheConfig(objectList, c.controller, c.resources); err != nil {
					t.Fatalf("addControllerCacheConfig(%q) failed: %v", c.controller, err)
				}
			}

			if len(objectList) != tt.wantEntries {
				t.Errorf("expected %d entries, got %d", tt.wantEntries, len(objectList))
			}

			_, byObj, found := findExistingCacheEntry(objectList, &appsv1.Deployment{})
			if !found {
				t.Fatal("expected Deployment entry in object list")
			}
			for _, value := range tt.wantMatch {
				if !byObj.Label.Matches(managedLabel(value)) {
					t.Errorf("expected selector to match %q, selector: %s", value, byObj.Label.String())
				}
			}
			for _, value := range tt.wantNotMatch {
				if byObj.Label.Matches(managedLabel(value)) {
					t.Errorf("expected selector NOT to match %q, selector: %s", value, byObj.Label.String())
				}
			}
		})
	}
}

func TestFindExistingCacheEntry(t *testing.T) {
	tests := []struct {
		name       string
		objectList map[client.Object]cache.ByObject
		lookup     client.Object
		wantFound  bool
	}{
		{
			name: "returns entry when type found",
			objectList: map[client.Object]cache.ByObject{
				&appsv1.Deployment{}:  {},
				&corev1.Service{}:     {},
				&rbacv1.ClusterRole{}: {},
			},
			lookup:    &appsv1.Deployment{},
			wantFound: true,
		},
		{
			name: "returns not-found when type absent",
			objectList: map[client.Object]cache.ByObject{
				&appsv1.Deployment{}: {},
				&corev1.Service{}:    {},
			},
			lookup: &rbacv1.ClusterRole{},
		},
		{
			name: "distinguishes different pointer types of same kind",
			objectList: map[client.Object]cache.ByObject{
				&rbacv1.ClusterRole{}: {},
			},
			lookup: &rbacv1.ClusterRoleBinding{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, _, found := findExistingCacheEntry(tt.objectList, tt.lookup)
			if found != tt.wantFound {
				t.Fatalf("expected found=%v for %T, got %v", tt.wantFound, tt.lookup, found)
			}
			if found && reflect.TypeOf(key) != reflect.TypeOf(tt.lookup) {
				t.Errorf("expected key to be %T, got %T", tt.lookup, key)
			}
		})
	}
}
