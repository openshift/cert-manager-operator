package operator

import (
	"reflect"
	"testing"

	certmanagerv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1alpha1 "github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/approverpolicy"
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

func TestControllerConfigAndCacheCombinations(t *testing.T) {
	tests := []struct {
		name               string
		config             ControllerConfig
		wantManagerEnabled bool
	}{
		{
			name:               "all controllers disabled",
			config:             ControllerConfig{},
			wantManagerEnabled: false,
		},
		{
			name:               "IstioCSR only",
			config:             ControllerConfig{EnableIstioCSR: true},
			wantManagerEnabled: true,
		},
		{
			name:               "TrustManager only",
			config:             ControllerConfig{EnableTrustManager: true},
			wantManagerEnabled: true,
		},
		{
			name:               "ApproverPolicyManager only",
			config:             ControllerConfig{EnableApproverPolicyManager: true},
			wantManagerEnabled: true,
		},
		{
			name:               "IstioCSR and TrustManager",
			config:             ControllerConfig{EnableIstioCSR: true, EnableTrustManager: true},
			wantManagerEnabled: true,
		},
		{
			name:               "IstioCSR and ApproverPolicyManager",
			config:             ControllerConfig{EnableIstioCSR: true, EnableApproverPolicyManager: true},
			wantManagerEnabled: true,
		},
		{
			name:               "TrustManager and ApproverPolicyManager",
			config:             ControllerConfig{EnableTrustManager: true, EnableApproverPolicyManager: true},
			wantManagerEnabled: true,
		},
		{
			name: "all controllers enabled",
			config: ControllerConfig{
				EnableIstioCSR:              true,
				EnableTrustManager:          true,
				EnableApproverPolicyManager: true,
			},
			wantManagerEnabled: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// The manager is needed whenever at least one controller is enabled.
			assert.Equal(t, tt.wantManagerEnabled, tt.config.enabled())
			objects, err := buildCacheObjectList(tt.config)
			require.NoError(t, err)
			if !tt.wantManagerEnabled {
				assert.Empty(t, objects, "disabled controllers must not configure any cache entries")
			}

			// Controllers sharing a resource type must share its cache entry.
			seen := map[reflect.Type]bool{}
			for object := range objects {
				resourceType := reflect.TypeOf(object)
				assert.False(t, seen[resourceType], "duplicate cache entry for %s", resourceType)
				seen[resourceType] = true
			}

			controllers := []struct {
				enabled         bool
				primaryResource client.Object
				managedLabel    string
			}{
				{
					enabled:         tt.config.EnableIstioCSR,
					primaryResource: &v1alpha1.IstioCSR{},
					managedLabel:    istiocsr.RequestEnqueueLabelValue,
				},
				{
					enabled:         tt.config.EnableTrustManager,
					primaryResource: &v1alpha1.TrustManager{},
					managedLabel:    trustmanager.RequestEnqueueLabelValue,
				},
				{
					enabled:         tt.config.EnableApproverPolicyManager,
					primaryResource: &v1alpha1.ApproverPolicyManager{},
					managedLabel:    approverpolicy.RequestEnqueueLabelValue,
				},
			}

			// Only enabled controllers get primary CR entries, without label filters.
			expectedCacheEntries := 0
			for _, controller := range controllers {
				if controller.enabled {
					expectedCacheEntries++
				}
				_, entry, found := findExistingCacheEntry(objects, controller.primaryResource)
				assert.Equal(t, controller.enabled, found, "cache entry for %T", controller.primaryResource)
				if found {
					assert.Nil(t, entry.Label, "%T must be unfiltered", controller.primaryResource)
				}
			}

			// List the expected resources independently of the production resource lists.
			// All three controllers share these types, so each selector must accept
			// exactly the enabled controllers' labels.
			sharedResources := []client.Object{
				&appsv1.Deployment{},
				&corev1.Service{},
				&corev1.ServiceAccount{},
				&rbacv1.ClusterRole{},
				&rbacv1.ClusterRoleBinding{},
				&rbacv1.Role{},
				&rbacv1.RoleBinding{},
			}
			if tt.wantManagerEnabled {
				expectedCacheEntries += len(sharedResources)
			}
			for _, obj := range sharedResources {
				_, entry, found := findExistingCacheEntry(objects, obj)
				assert.Equal(t, tt.wantManagerEnabled, found, "cache entry for %T", obj)
				if found {
					require.NotNil(t, entry.Label, "%T must have a label filter", obj)
					for _, controller := range controllers {
						matches := entry.Label.Matches(labels.Set{common.ManagedResourceLabelKey: controller.managedLabel})
						assert.Equal(t, controller.enabled, matches, "%T selector for %T", obj, controller.primaryResource)
					}
					assert.False(t, entry.Label.Matches(labels.Set{}), "exclude unlabeled %T", obj)
					assert.False(t, entry.Label.Matches(labels.Set{common.ManagedResourceLabelKey: "unrelated"}), "exclude unrelated %T", obj)
				}
			}

			// These resources must keep using the default, unfiltered shared informer.
			unfilteredResources := []client.Object{
				&corev1.ConfigMap{},
				&certmanagerv1.Issuer{},
				&certmanagerv1.ClusterIssuer{},
				&corev1.Secret{},
				&metav1.PartialObjectMetadata{},
			}
			for _, obj := range unfilteredResources {
				_, _, found := findExistingCacheEntry(objects, obj)
				assert.False(t, found, "%T must retain its unfiltered shared informer", obj)
			}

			// These types belong to a subset of controllers. Other controllers' labels
			// must be rejected even when those controllers are enabled.
			controllerSpecificResources := []struct {
				object             client.Object
				wantIstioCSR       bool
				wantTrustManager   bool
				wantApproverPolicy bool
			}{
				{
					object:           &certmanagerv1.Certificate{},
					wantIstioCSR:     tt.config.EnableIstioCSR,
					wantTrustManager: tt.config.EnableTrustManager,
				},
				{
					object:       &networkingv1.NetworkPolicy{},
					wantIstioCSR: tt.config.EnableIstioCSR,
				},
				{
					object:             &admissionregistrationv1.ValidatingWebhookConfiguration{},
					wantTrustManager:   tt.config.EnableTrustManager,
					wantApproverPolicy: tt.config.EnableApproverPolicyManager,
				},
			}
			for _, resource := range controllerSpecificResources {
				wantEntry := resource.wantIstioCSR || resource.wantTrustManager || resource.wantApproverPolicy
				if wantEntry {
					expectedCacheEntries++
				}
				_, entry, found := findExistingCacheEntry(objects, resource.object)
				assert.Equal(t, wantEntry, found, "cache entry for %T", resource.object)
				if found {
					require.NotNil(t, entry.Label, "%T must have a label filter", resource.object)
					assert.Equal(t, resource.wantIstioCSR, entry.Label.Matches(labels.Set{common.ManagedResourceLabelKey: istiocsr.RequestEnqueueLabelValue}), "%T IstioCSR selector", resource.object)
					assert.Equal(t, resource.wantTrustManager, entry.Label.Matches(labels.Set{common.ManagedResourceLabelKey: trustmanager.RequestEnqueueLabelValue}), "%T TrustManager selector", resource.object)
					assert.Equal(t, resource.wantApproverPolicy, entry.Label.Matches(labels.Set{common.ManagedResourceLabelKey: approverpolicy.RequestEnqueueLabelValue}), "%T ApproverPolicyManager selector", resource.object)
					assert.False(t, entry.Label.Matches(labels.Set{}), "exclude unlabeled %T", resource.object)
					assert.False(t, entry.Label.Matches(labels.Set{common.ManagedResourceLabelKey: "unrelated"}), "exclude unrelated %T", resource.object)
				}
			}

			assert.Len(t, objects, expectedCacheEntries, "cache must contain only the expected resource types")
		})
	}
}
