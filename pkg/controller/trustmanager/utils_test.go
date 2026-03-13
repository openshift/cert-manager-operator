package trustmanager

import (
	"testing"
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
