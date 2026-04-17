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
		checkLabels map[string]string
		description string
	}{
		{
			name: "happy path - set labels",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "test"},
			},
			labels:      map[string]string{"key": "value", "a": "b"},
			checkLabels: map[string]string{"key": "value", "a": "b"},
			description: "labels replace existing",
		},
		{
			name: "edge case - nil labels",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"old": "v"}},
			},
			labels:      nil,
			checkLabels: nil,
			description: "nil labels clears (SetLabels(nil) behavior)",
		},
		{
			name: "edge case - empty map",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: "cm", Labels: map[string]string{"a": "1"}},
			},
			labels:      map[string]string{},
			checkLabels: map[string]string{},
			description: "empty map clears labels",
		},
		{
			name:        "single element",
			obj:         &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "s"}},
			labels:      map[string]string{"only": "one"},
			checkLabels: map[string]string{"only": "one"},
			description: "single label",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			UpdateResourceLabels(tt.obj, tt.labels)
			got := tt.obj.GetLabels()
			require.Len(t, got, len(tt.checkLabels), tt.description)
			for k, v := range tt.checkLabels {
				assert.Equal(t, v, got[k], "label %q", k)
			}
		})
	}
}

func TestObjectMetadataModified(t *testing.T) {
	tests := []struct {
		name        string
		desired     client.Object
		fetched     client.Object
		wantChanged bool
	}{
		{
			name:        "different kinds both empty labels",
			desired:     &corev1.ServiceAccount{},
			fetched:     &corev1.ConfigMap{},
			wantChanged: false,
		},
		{
			name: "ServiceAccount same labels",
			desired: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "1"}},
			},
			fetched: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "1"}},
			},
			wantChanged: false,
		},
		{
			name: "ServiceAccount different label value",
			desired: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "1"}},
			},
			fetched: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Name: "x", Labels: map[string]string{"a": "2"}},
			},
			wantChanged: true,
		},
		{
			name: "ServiceAccount different label key",
			desired: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			},
			fetched: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"b": "1"}},
			},
			wantChanged: true,
		},
		{
			name:        "ServiceAccount both nil labels",
			desired:     &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: nil}},
			fetched:     &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: nil}},
			wantChanged: false,
		},
		{
			name:    "ServiceAccount desired nil labels fetched has labels",
			desired: &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: nil}},
			fetched: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			},
			wantChanged: true,
		},
		{
			name: "ServiceAccount desired has labels fetched nil labels",
			desired: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"a": "1"}},
			},
			fetched:     &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: nil}},
			wantChanged: true,
		},
		{
			name: "Deployment different labels",
			desired: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "1"}},
			},
			fetched: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "2"}},
			},
			wantChanged: true,
		},
		{
			name: "Deployment same labels",
			desired: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "1"}},
			},
			fetched: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "deploy", Labels: map[string]string{"a": "1"}},
			},
			wantChanged: false,
		},
		{
			name: "ConfigMap different labels",
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

func TestContainsAnnotation(t *testing.T) {
	tests := []struct {
		name        string
		obj         client.Object
		annotation  string
		wantPresent bool
	}{
		{
			name: "present key",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"present": "yes"}},
			},
			annotation:  "present",
			wantPresent: true,
		},
		{
			name: "absent key",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{Annotations: map[string]string{"present": "yes"}},
			},
			annotation:  "absent",
			wantPresent: false,
		},
		{
			name:        "nil annotations",
			obj:         &corev1.ServiceAccount{},
			annotation:  "any",
			wantPresent: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ContainsAnnotation(tt.obj, tt.annotation)
			assert.Equal(t, tt.wantPresent, got)
		})
	}
}

func TestAddAnnotation(t *testing.T) {
	tests := []struct {
		name      string
		obj       *corev1.ServiceAccount
		key       string
		value     string
		wantAdded bool
		wantVal   string
	}{
		{
			name:      "adds when missing",
			obj:       &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{}},
			key:       "key",
			value:     "value",
			wantAdded: true,
			wantVal:   "value",
		},
		{
			name: "no change when present",
			obj: &corev1.ServiceAccount{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{"key": "existing"},
				},
			},
			key:       "key",
			value:     "new",
			wantAdded: false,
			wantVal:   "existing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added := AddAnnotation(tt.obj, tt.key, tt.value)
			assert.Equal(t, tt.wantAdded, added)
			assert.Equal(t, tt.wantVal, tt.obj.GetAnnotations()[tt.key])
		})
	}
}
