package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TestUpdateNamespace provides table-driven tests for UpdateNamespace(obj, newNamespace).
// Refactor may move this to ctrlutil; tests ensure behavior (side-effect on obj) is preserved.
func TestUpdateNamespace(t *testing.T) {
	tests := []struct {
		name         string
		obj          client.Object
		newNamespace string
		expectedNs   string
		description  string
	}{
		{
			name: "happy path - set new namespace",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: "old-ns"},
			},
			newNamespace: "new-ns",
			expectedNs:   "new-ns",
			description:  "namespace is updated in place",
		},
		{
			name: "edge case - empty string namespace",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "default"},
			},
			newNamespace: "",
			expectedNs:   "",
			description:  "empty namespace is allowed",
		},
		{
			name: "boundary - long namespace name",
			obj: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "s", Namespace: "ns"},
			},
			newNamespace: "openshift-cert-manager-operator",
			expectedNs:   "openshift-cert-manager-operator",
			description:  "long namespace is set correctly",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UpdateNamespace(tt.obj, tt.newNamespace)
			assert.Equal(t, tt.expectedNs, tt.obj.GetNamespace(), tt.description)
		})
	}
}

// TestUpdateResourceLabels provides table-driven tests for UpdateResourceLabels(obj, labels).
func TestUpdateResourceLabels(t *testing.T) {
	tests := []struct {
		name        string
		obj         client.Object
		labels      map[string]string
		expectedLen int
		checkLabels map[string]string
		description string
	}{
		{
			name: "happy path - set labels",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			labels:      map[string]string{"key": "value", "a": "b"},
			expectedLen: 2,
			checkLabels: map[string]string{"key": "value", "a": "b"},
			description: "labels replace existing",
		},
		{
			name: "edge case - nil labels",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"old": "v"}},
			},
			labels:      nil,
			expectedLen: 0,
			checkLabels: nil,
			description: "nil labels clears (SetLabels(nil) behavior)",
		},
		{
			name: "edge case - empty map",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cm", Labels: map[string]string{"a": "1"}},
			},
			labels:      map[string]string{},
			expectedLen: 0,
			checkLabels: map[string]string{},
			description: "empty map clears labels",
		},
		{
			name:        "single element",
			obj:         &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s"}},
			labels:      map[string]string{"only": "one"},
			expectedLen: 1,
			checkLabels: map[string]string{"only": "one"},
			description: "single label",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UpdateResourceLabels(tt.obj, tt.labels)
			got := tt.obj.GetLabels()
			require.Len(t, got, tt.expectedLen, tt.description)
			for k, v := range tt.checkLabels {
				assert.Equal(t, v, got[k], "label %q", k)
			}
		})
	}
}

func TestObjectMetadataModified_DifferentTypes(t *testing.T) {
	sa := &corev1.ServiceAccount{}
	cm := &corev1.ConfigMap{}
	if ObjectMetadataModified(sa, cm) {
		t.Error("ObjectMetadataModified should return false when both objects have the same labels (including both empty)")
	}
}

func TestObjectMetadataModified_SameTypeDifferentLabels(t *testing.T) {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "1"}},
	}
	fetched := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "2"}},
	}
	if !ObjectMetadataModified(desired, fetched) {
		t.Error("ObjectMetadataModified should return true when labels differ")
	}
}

func TestObjectMetadataModified_SameTypeSameLabels(t *testing.T) {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "1"}},
	}
	fetched := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "1"}},
	}
	if ObjectMetadataModified(desired, fetched) {
		t.Error("ObjectMetadataModified should return false when labels match")
	}
}

func TestObjectMetadataModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  map[string]string
		fetched  map[string]string
		wantDiff bool
	}{
		{"same", map[string]string{"a": "1"}, map[string]string{"a": "1"}, false},
		{"different value", map[string]string{"a": "1"}, map[string]string{"a": "2"}, true},
		{"different key", map[string]string{"a": "1"}, map[string]string{"b": "1"}, true},
		{"both nil", nil, nil, false},
		{"desired nil fetched has labels", nil, map[string]string{"a": "1"}, true},
		{"desired has labels fetched nil", map[string]string{"a": "1"}, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.desired}}
			f := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.fetched}}
			got := ObjectMetadataModified(d, f)
			if got != tt.wantDiff {
				t.Errorf("ObjectMetadataModified() = %v, want %v", got, tt.wantDiff)
			}
		})
	}
}

func TestContainsAnnotation(t *testing.T) {
	obj := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{"present": "yes"},
		},
	}
	if !ContainsAnnotation(obj, "present") {
		t.Error("ContainsAnnotation should be true for present key")
	}
	if ContainsAnnotation(obj, "absent") {
		t.Error("ContainsAnnotation should be false for absent key")
	}
	empty := &corev1.ServiceAccount{}
	if ContainsAnnotation(empty, "any") {
		t.Error("ContainsAnnotation should be false when annotations nil")
	}
}

func TestAddAnnotation(t *testing.T) {
	t.Run("adds when missing", func(t *testing.T) {
		obj := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{}}
		added := AddAnnotation(obj, "key", "value")
		if !added {
			t.Error("AddAnnotation should return true when adding")
		}
		if obj.GetAnnotations()["key"] != "value" {
			t.Errorf("annotation value = %q, want %q", obj.GetAnnotations()["key"], "value")
		}
	})
	t.Run("no change when present", func(t *testing.T) {
		obj := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{"key": "existing"},
			},
		}
		added := AddAnnotation(obj, "key", "new")
		if added {
			t.Error("AddAnnotation should return false when already present")
		}
		if obj.GetAnnotations()["key"] != "existing" {
			t.Errorf("annotation should be unchanged: %q", obj.GetAnnotations()["key"])
		}
	})
}

// Ensure we can pass any client.Object to ObjectMetadataModified (e.g. *appsv1.Deployment).
func TestObjectMetadataModified_WithDifferentObjectTypes(t *testing.T) {
	tests := []struct {
		name        string
		desired     client.Object
		fetched     client.Object
		wantChanged bool
	}{
		{
			name: "Deployment with different labels",
			desired: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "1"}},
			},
			fetched: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "2"}},
			},
			wantChanged: true,
		},
		{
			name: "Deployment with same labels",
			desired: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "1"}},
			},
			fetched: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "1"}},
			},
			wantChanged: false,
		},
		{
			name: "ConfigMap with different labels",
			desired: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cm", Labels: map[string]string{"key": "val1"}},
			},
			fetched: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cm", Labels: map[string]string{"key": "val2"}},
			},
			wantChanged: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ObjectMetadataModified(tt.desired, tt.fetched)
			assert.Equal(t, tt.wantChanged, got)
		})
	}
}
