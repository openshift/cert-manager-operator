package trustmanager

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/common"
)

func TestGetTrustNamespace(t *testing.T) {
	tests := []struct {
		name           string
		trustNamespace string
		expected       string
	}{
		{
			name:           "returns configured namespace",
			trustNamespace: "custom-ns",
			expected:       "custom-ns",
		},
		{
			name:           "returns default when empty",
			trustNamespace: "",
			expected:       defaultTrustNamespace,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := testTrustManager().Build()
			tm.Spec.TrustManagerConfig.TrustNamespace = tt.trustNamespace
			result := getTrustNamespace(tm)
			if result != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, result)
			}
		})
	}
}

func TestGetResourceLabels(t *testing.T) {
	tests := []struct {
		name       string
		tm         *trustManagerBuilder
		wantLabels map[string]string
	}{
		{
			name: "default labels are always present",
			tm:   testTrustManager(),
			wantLabels: map[string]string{
				"app":                          trustManagerCommonName,
				"app.kubernetes.io/managed-by": "cert-manager-operator",
			},
		},
		{
			name: "user labels are merged",
			tm:   testTrustManager().WithLabels(map[string]string{"user-label": "test-value"}),
			wantLabels: map[string]string{
				"user-label": "test-value",
			},
		},
		{
			name: "default labels take precedence over user labels",
			tm:   testTrustManager().WithLabels(map[string]string{"app": "should-be-overridden"}),
			wantLabels: map[string]string{
				"app": trustManagerCommonName,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			labels := getResourceLabels(tt.tm.Build())
			for key, val := range tt.wantLabels {
				if labels[key] != val {
					t.Errorf("expected label %s=%q, got %q", key, val, labels[key])
				}
			}
		})
	}
}

func TestGetResourceAnnotations(t *testing.T) {
	tests := []struct {
		name            string
		tm              *trustManagerBuilder
		wantAnnotations map[string]string
	}{
		{
			name: "empty when no custom annotations",
			tm:   testTrustManager(),
		},
		{
			name: "user annotations are returned",
			tm:   testTrustManager().WithAnnotations(map[string]string{"user-annotation": "test-value"}),
			wantAnnotations: map[string]string{
				"user-annotation": "test-value",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			annotations := getResourceAnnotations(tt.tm.Build())
			if len(tt.wantAnnotations) == 0 && len(annotations) != 0 {
				t.Errorf("expected empty annotations, got %v", annotations)
			}
			for key, val := range tt.wantAnnotations {
				if annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, annotations[key])
				}
			}
		})
	}
}

func TestManagedLabelsModified(t *testing.T) {
	tests := []struct {
		name     string
		desired  map[string]string
		existing map[string]string
		want     bool
	}{
		{
			name:     "identical labels returns not modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "1"},
			want:     false,
		},
		{
			name:     "different value for same key returns modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "2"},
			want:     true,
		},
		{
			name:     "existing has extra labels beyond desired still not modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{"a": "1", "b": "2"},
			want:     false,
		},
		{
			name:     "desired label missing on existing returns modified",
			desired:  map[string]string{"a": "1"},
			existing: map[string]string{},
			want:     true,
		},
		{
			name:     "nil desired labels returns not modified",
			desired:  nil,
			existing: map[string]string{"a": "1"},
			want:     false,
		},
		{
			name:     "both nil labels returns not modified",
			desired:  nil,
			existing: nil,
			want:     false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.desired}}
			existing := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.existing}}
			got := managedLabelsModified(desired, existing)
			if got != tt.want {
				t.Errorf("managedLabelsModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

// buildManagedFieldsJSON constructs a FieldsV1 JSON blob for testing.
// It accepts label keys and annotation keys that the field manager "owns".
func buildManagedFieldsJSON(t *testing.T, labelKeys, annotationKeys []string) []byte {
	t.Helper()

	metadata := map[string]interface{}{}

	if len(labelKeys) > 0 {
		labels := map[string]interface{}{}
		for _, k := range labelKeys {
			labels["f:"+k] = map[string]interface{}{}
		}
		metadata["f:labels"] = labels
	}
	if len(annotationKeys) > 0 {
		annotations := map[string]interface{}{}
		for _, k := range annotationKeys {
			annotations["f:"+k] = map[string]interface{}{}
		}
		metadata["f:annotations"] = annotations
	}

	raw, err := json.Marshal(map[string]interface{}{
		"f:metadata": metadata,
	})
	if err != nil {
		t.Fatalf("failed to build managedFields JSON: %v", err)
	}
	return raw
}

func TestManagedLabelsModifiedWithManagedFields(t *testing.T) {
	tests := []struct {
		name          string
		desired       map[string]string
		existing      map[string]string
		managedFields []metav1.ManagedFieldsEntry
		want          bool
	}{
		{
			name:     "stale owned label removed from desired returns modified",
			desired:  map[string]string{"app": "x"},
			existing: map[string]string{"app": "x", "team": "platform"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"app", "team"}, nil)},
				},
			},
			want: true,
		},
		{
			name:     "third-party label not owned is ignored",
			desired:  map[string]string{"app": "x"},
			existing: map[string]string{"app": "x", "extra": "v"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"app"}, nil)},
				},
			},
			want: false,
		},
		{
			name:     "all owned labels present returns not modified",
			desired:  map[string]string{"app": "x", "team": "platform"},
			existing: map[string]string{"app": "x", "team": "platform"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"app", "team"}, nil)},
				},
			},
			want: false,
		},
		{
			name:     "owned by different manager is ignored",
			desired:  map[string]string{"app": "x"},
			existing: map[string]string{"app": "x", "other": "v"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   "some-other-controller",
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"app", "other"}, nil)},
				},
			},
			want: false,
		},
		{
			name:          "empty managedFields returns not modified when forward check passes",
			desired:       map[string]string{"app": "x"},
			existing:      map[string]string{"app": "x", "stale": "v"},
			managedFields: nil,
			want:          false,
		},
		{
			name:     "non-Apply operation entries are ignored",
			desired:  map[string]string{"app": "x"},
			existing: map[string]string{"app": "x", "team": "platform"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationUpdate,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"app", "team"}, nil)},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Labels: tt.desired}}
			existing := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
				Labels:        tt.existing,
				ManagedFields: tt.managedFields,
			}}
			got := managedLabelsModified(desired, existing)
			if got != tt.want {
				t.Errorf("managedLabelsModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManagedAnnotationsModifiedWithManagedFields(t *testing.T) {
	tests := []struct {
		name          string
		desired       map[string]string
		existing      map[string]string
		managedFields []metav1.ManagedFieldsEntry
		want          bool
	}{
		{
			name:     "stale owned annotation removed from desired returns modified",
			desired:  map[string]string{"keep": "v"},
			existing: map[string]string{"keep": "v", "stale": "old"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, nil, []string{"keep", "stale"})},
				},
			},
			want: true,
		},
		{
			name:     "third-party annotation not owned is ignored",
			desired:  map[string]string{"keep": "v"},
			existing: map[string]string{"keep": "v", "other-controller/note": "x"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, nil, []string{"keep"})},
				},
			},
			want: false,
		},
		{
			name:     "all owned annotations present returns not modified",
			desired:  map[string]string{"a": "1", "b": "2"},
			existing: map[string]string{"a": "1", "b": "2"},
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, nil, []string{"a", "b"})},
				},
			},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Annotations: tt.desired}}
			existing := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{
				Annotations:   tt.existing,
				ManagedFields: tt.managedFields,
			}}
			got := managedAnnotationsModified(desired, existing)
			if got != tt.want {
				t.Errorf("managedAnnotationsModified() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOwnedMetadataKeys(t *testing.T) {
	tests := []struct {
		name            string
		managedFields   []metav1.ManagedFieldsEntry
		manager         string
		wantLabels      map[string]struct{}
		wantAnnotations map[string]struct{}
	}{
		{
			name:            "nil managedFields returns empty sets",
			managedFields:   nil,
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{},
			wantAnnotations: map[string]struct{}{},
		},
		{
			name: "extracts labels and annotations from matching manager",
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"app", "team"}, []string{"note"})},
				},
			},
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{"app": {}, "team": {}},
			wantAnnotations: map[string]struct{}{"note": {}},
		},
		{
			name: "ignores entries from other managers",
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   "other-controller",
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"foreign"}, nil)},
				},
			},
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{},
			wantAnnotations: map[string]struct{}{},
		},
		{
			name: "ignores Update operation entries",
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationUpdate,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"updated-label"}, nil)},
				},
			},
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{},
			wantAnnotations: map[string]struct{}{},
		},
		{
			name: "handles nil FieldsV1 gracefully",
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  nil,
				},
			},
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{},
			wantAnnotations: map[string]struct{}{},
		},
		{
			name: "handles invalid JSON gracefully (fail-open)",
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: []byte("not valid json")},
				},
			},
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{},
			wantAnnotations: map[string]struct{}{},
		},
		{
			name: "merges keys from multiple Apply entries by same manager",
			managedFields: []metav1.ManagedFieldsEntry{
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"a"}, []string{"x"})},
				},
				{
					Manager:   fieldOwner,
					Operation: metav1.ManagedFieldsOperationApply,
					FieldsV1:  &metav1.FieldsV1{Raw: buildManagedFieldsJSON(t, []string{"b"}, []string{"y"})},
				},
			},
			manager:         fieldOwner,
			wantLabels:      map[string]struct{}{"a": {}, "b": {}},
			wantAnnotations: map[string]struct{}{"x": {}, "y": {}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			obj := &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{ManagedFields: tt.managedFields}}
			gotLabels, gotAnnotations := ownedMetadataKeys(obj, tt.manager)
			if !mapsEqualStruct(gotLabels, tt.wantLabels) {
				t.Errorf("ownedMetadataKeys() labels = %v, want %v", gotLabels, tt.wantLabels)
			}
			if !mapsEqualStruct(gotAnnotations, tt.wantAnnotations) {
				t.Errorf("ownedMetadataKeys() annotations = %v, want %v", gotAnnotations, tt.wantAnnotations)
			}
		})
	}
}

func TestManagedFieldsDeleted(t *testing.T) {
	tests := []struct {
		name        string
		ownedKeys   map[string]struct{}
		desiredKeys map[string]string
		want        bool
	}{
		{
			name:        "no owned keys returns false",
			ownedKeys:   map[string]struct{}{},
			desiredKeys: map[string]string{"a": "1"},
			want:        false,
		},
		{
			name:        "all owned keys present in desired returns false",
			ownedKeys:   map[string]struct{}{"a": {}, "b": {}},
			desiredKeys: map[string]string{"a": "1", "b": "2", "c": "3"},
			want:        false,
		},
		{
			name:        "owned key missing from desired returns true",
			ownedKeys:   map[string]struct{}{"a": {}, "b": {}},
			desiredKeys: map[string]string{"a": "1"},
			want:        true,
		},
		{
			name:        "nil desired with owned keys returns true",
			ownedKeys:   map[string]struct{}{"a": {}},
			desiredKeys: nil,
			want:        true,
		},
		{
			name:        "nil owned and nil desired returns false",
			ownedKeys:   map[string]struct{}{},
			desiredKeys: nil,
			want:        false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := managedFieldsDeleted(tt.ownedKeys, tt.desiredKeys)
			if got != tt.want {
				t.Errorf("managedFieldsDeleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

// mapsEqualStruct compares two map[string]struct{} for equality.
func mapsEqualStruct(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

func TestValidateTrustManagerConfig(t *testing.T) {
	tests := []struct {
		name    string
		tm      *trustManagerBuilder
		wantErr string
	}{
		{
			name: "valid config with defaults passes",
			tm:   testTrustManager(),
		},
		{
			name: "empty TrustManagerConfig is rejected",
			tm: func() *trustManagerBuilder {
				b := testTrustManager()
				b.Spec.TrustManagerConfig = v1alpha1.TrustManagerConfig{}
				return b
			}(),
			wantErr: "spec.trustManagerConfig config cannot be empty",
		},
		{
			name: "valid custom labels pass",
			tm:   testTrustManager().WithLabels(map[string]string{"app.kubernetes.io/team": "platform"}),
		},
		{
			name:    "invalid label key is rejected",
			tm:      testTrustManager().WithLabels(map[string]string{"invalid/key/with/extra/slash": "val"}),
			wantErr: `spec.controllerConfig.labels: Invalid value:`,
		},
		{
			name: "valid custom annotations pass",
			tm:   testTrustManager().WithAnnotations(map[string]string{"example.com/note": "test"}),
		},
		{
			name:    "invalid annotation key is rejected",
			tm:      testTrustManager().WithAnnotations(map[string]string{"invalid/key/with/extra/slash": "val"}),
			wantErr: `spec.controllerConfig.annotations: Invalid value:`,
		},
		{
			name: "non-empty trustManagerConfig with explicit fields is valid",
			tm: func() *trustManagerBuilder {
				b := testTrustManager()
				b.Spec.TrustManagerConfig = v1alpha1.TrustManagerConfig{
					LogLevel:       2,
					LogFormat:      "json",
					TrustNamespace: "cert-manager",
				}
				return b
			}(),
		},
		{
			name:    "reserved label key 'app' is rejected",
			tm:      testTrustManager().WithLabels(map[string]string{"app": "custom-value"}),
			wantErr: `key "app" is reserved for controller use`,
		},
		{
			name:    "reserved label key 'app.kubernetes.io/managed-by' is rejected",
			tm:      testTrustManager().WithLabels(map[string]string{"app.kubernetes.io/managed-by": "other"}),
			wantErr: `key "app.kubernetes.io/managed-by" is reserved for controller use`,
		},
		{
			name:    "reserved annotation key is rejected",
			tm:      testTrustManager().WithAnnotations(map[string]string{"cert-manager.io/inject-ca-from-secret": "wrong"}),
			wantErr: `key "cert-manager.io/inject-ca-from-secret" is reserved for controller use`,
		},
		{
			name: "valid custom label and annotation pass",
			tm: testTrustManager().
				WithLabels(map[string]string{"team": "platform"}).
				WithAnnotations(map[string]string{"example.com/note": "test"}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrustManagerConfig(tt.tm.Build())
			assertError(t, err, tt.wantErr)
		})
	}
}

func TestValidateReservedKeys(t *testing.T) {
	tests := []struct {
		name     string
		userKeys map[string]string
		reserved map[string]struct{}
		wantErr  string
	}{
		{
			name:     "no conflict returns nil",
			userKeys: map[string]string{"custom": "val"},
			reserved: map[string]struct{}{"app": {}},
			wantErr:  "",
		},
		{
			name:     "single reserved key is rejected",
			userKeys: map[string]string{"app": "custom"},
			reserved: map[string]struct{}{"app": {}},
			wantErr:  `key "app" is reserved for controller use`,
		},
		{
			name:     "multiple reserved keys are rejected",
			userKeys: map[string]string{"app": "a", "app.kubernetes.io/managed-by": "b"},
			reserved: map[string]struct{}{"app": {}, "app.kubernetes.io/managed-by": {}},
			wantErr:  "is reserved for controller use",
		},
		{
			name:     "empty user keys returns nil",
			userKeys: map[string]string{},
			reserved: map[string]struct{}{"app": {}},
			wantErr:  "",
		},
		{
			name:     "empty reserved set returns nil",
			userKeys: map[string]string{"app": "val"},
			reserved: map[string]struct{}{},
			wantErr:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateReservedKeys(tt.userKeys, tt.reserved, controllerConfigFieldPath, "labels")
			assertError(t, err, tt.wantErr)
		})
	}
}

func TestUpdateResourceAnnotations(t *testing.T) {
	tests := []struct {
		name                string
		existingAnnotations map[string]string
		inputAnnotations    map[string]string
		wantAnnotations     map[string]string
	}{
		{
			name:                "no-op when input annotations are empty",
			existingAnnotations: map[string]string{"existing": "value"},
			inputAnnotations:    nil,
			wantAnnotations:     map[string]string{"existing": "value"},
		},
		{
			name:                "user annotations take precedence over existing",
			existingAnnotations: map[string]string{"key": "original"},
			inputAnnotations:    map[string]string{"key": "overridden"},
			wantAnnotations:     map[string]string{"key": "overridden"},
		},
		{
			name:                "initializes annotations map if nil",
			existingAnnotations: nil,
			inputAnnotations:    map[string]string{"new": "value"},
			wantAnnotations:     map[string]string{"new": "value"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := testTrustManager().Build()
			tm.SetAnnotations(tt.existingAnnotations)
			updateResourceAnnotations(tm, tt.inputAnnotations)
			for key, val := range tt.wantAnnotations {
				if tm.Annotations[key] != val {
					t.Errorf("expected annotation %s=%q, got %q", key, val, tm.Annotations[key])
				}
			}
		})
	}
}

func mustEncode(t *testing.T, encoder runtime.Encoder, obj runtime.Object) []byte {
	t.Helper()
	b, err := runtime.Encode(encoder, obj)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return b
}

// TestDecodeServiceAccountObjBytes exercises common.DecodeObjBytes for ServiceAccount assets (same path as serviceaccounts.go).
func TestDecodeServiceAccountObjBytes(t *testing.T) {
	validSA := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "test-sa", Namespace: "test-ns"},
	}
	configMapObj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: "cm", Namespace: "ns"},
		Data:       map[string]string{"k": "v"},
	}
	gv := corev1.SchemeGroupVersion
	encoder := codecs.LegacyCodec(gv)
	tests := []struct {
		name          string
		getBytes      func(t *testing.T) []byte
		wantPanic     bool
		panicSubstr   string
		wantName      string
		wantNamespace string
	}{
		{
			name: "valid ServiceAccount bytes returns SA",
			getBytes: func(t *testing.T) []byte {
				return mustEncode(t, encoder, validSA)
			},
			wantPanic:     false,
			wantName:      "test-sa",
			wantNamespace: "test-ns",
		},
		{
			name: "wrong type bytes panics",
			getBytes: func(t *testing.T) []byte {
				return mustEncode(t, encoder, configMapObj)
			},
			wantPanic:   true,
			panicSubstr: "ServiceAccount",
		},
		{
			name: "invalid bytes panics",
			getBytes: func(t *testing.T) []byte {
				return []byte("not valid yaml or json")
			},
			wantPanic: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			objBytes := tt.getBytes(t)
			if tt.wantPanic {
				defer func() {
					r := recover()
					if r == nil {
						t.Error("expected panic")
					}
					if tt.panicSubstr != "" {
						msg := fmt.Sprintf("%v", r)
						if !strings.Contains(msg, tt.panicSubstr) && !strings.Contains(msg, "interface") {
							t.Errorf("panic message = %q, want substring %q", msg, tt.panicSubstr)
						}
					}
				}()
			}
			got := common.DecodeObjBytes[*corev1.ServiceAccount](codecs, gv, objBytes)
			if !tt.wantPanic {
				if got.Name != tt.wantName || got.Namespace != tt.wantNamespace {
					t.Errorf("DecodeObjBytes() = %s/%s, want %s/%s", got.Name, got.Namespace, tt.wantName, tt.wantNamespace)
				}
			}
		})
	}
}

func TestManagedAnnotationsModified(t *testing.T) {
	tests := []struct {
		name               string
		desiredAnnotations map[string]string
		currentAnnotations map[string]string
		wantModified       bool
	}{
		{
			name:               "identical annotations not modified",
			desiredAnnotations: map[string]string{"key": "value"},
			currentAnnotations: map[string]string{"key": "value"},
			wantModified:       false,
		},
		{
			name:               "missing managed annotation is modified",
			desiredAnnotations: map[string]string{"managed": "value"},
			currentAnnotations: map[string]string{"other": "value"},
			wantModified:       true,
		},
		{
			name:               "changed managed annotation is modified",
			desiredAnnotations: map[string]string{"key": "desired"},
			currentAnnotations: map[string]string{"key": "tampered"},
			wantModified:       true,
		},
		{
			name:               "extra annotation on existing is allowed",
			desiredAnnotations: map[string]string{"managed": "value"},
			currentAnnotations: map[string]string{"managed": "value", "extra": "ok"},
			wantModified:       false,
		},
		{
			name:               "nil desired annotations not modified",
			desiredAnnotations: nil,
			currentAnnotations: map[string]string{"any": "value"},
			wantModified:       false,
		},
		{
			name:               "nil existing annotations with desired is modified",
			desiredAnnotations: map[string]string{"key": "value"},
			currentAnnotations: nil,
			wantModified:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			desired := testTrustManager().Build()
			desired.SetAnnotations(tt.desiredAnnotations)

			existing := testTrustManager().Build()
			existing.SetAnnotations(tt.currentAnnotations)

			got := managedAnnotationsModified(desired, existing)
			if got != tt.wantModified {
				t.Errorf("expected modified=%v, got %v", tt.wantModified, got)
			}
		})
	}
}
