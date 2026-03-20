package applyconfigurations

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	managedfields "k8s.io/apimachinery/pkg/util/managedfields"
)

// TestNewTypeConverter ensures the type converter is created from the scheme and
// can be used for managed fields. Refactoring may change the implementation
// (e.g. internal.Parser()); these tests guard behavior.
func TestNewTypeConverter(t *testing.T) {
	tests := []struct {
		name        string
		scheme      *runtime.Scheme
		expectError bool
		description string
	}{
		{
			name:        "happy path - valid scheme returns non-nil converter",
			scheme:      runtime.NewScheme(),
			expectError: false,
			description: "NewTypeConverter must return a non-nil TypeConverter for a valid scheme",
		},
		{
			name:        "nil scheme - may panic or return converter",
			scheme:      nil,
			expectError: false,
			description: "Document behavior: nil scheme may panic in NewSchemeTypeConverter; refactor should preserve or explicitly reject",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var conv managedfields.TypeConverter
			didPanic := false
			func() {
				defer func() {
					if r := recover(); r != nil {
						didPanic = true
						if tt.scheme == nil {
							t.Logf("NewTypeConverter(nil) panicked: %v", r)
						}
					}
				}()
				conv = NewTypeConverter(tt.scheme)
			}()
			if tt.scheme != nil {
				require.False(t, didPanic, "NewTypeConverter(non-nil scheme) must not panic")
				require.NotNil(t, conv, "NewTypeConverter must not return nil for non-nil scheme")
			}
			// Document: for nil scheme, either conv is nil (if we returned) or we panicked.
			if !didPanic && conv != nil {
				assert.NotNil(t, conv, tt.description)
			}
		})
	}
}
