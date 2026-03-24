package trustmanager

import (
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
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateTrustManagerConfig(tt.tm.Build())
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
