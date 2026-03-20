package v1alpha1

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
)

// TestCertManagerConfigApplyConfiguration_WithIssuerRef provides table-driven tests for
// WithIssuerRef(value v1.IssuerReference). Refactor may change param type from ObjectReference to IssuerReference.
func TestCertManagerConfigApplyConfiguration_WithIssuerRef(t *testing.T) {
	tests := []struct {
		name     string
		input    cmmeta.IssuerReference
		chained  bool
		expected *cmmeta.IssuerReference
	}{
		{
			name: "happy path - set IssuerRef",
			input: cmmeta.IssuerReference{
				Name:  "my-issuer",
				Kind:  "ClusterIssuer",
				Group: "cert-manager.io",
			},
			chained: false,
			expected: &cmmeta.IssuerReference{
				Name:  "my-issuer",
				Kind:  "ClusterIssuer",
				Group: "cert-manager.io",
			},
		},
		{
			name:     "edge case - zero value IssuerRef",
			input:    cmmeta.IssuerReference{},
			chained:  false,
			expected: &cmmeta.IssuerReference{},
		},
		{
			name: "chained call returns receiver",
			input: cmmeta.IssuerReference{Name: "issuer", Kind: "Issuer", Group: "cert-manager.io"},
			chained: true,
			expected: &cmmeta.IssuerReference{Name: "issuer", Kind: "Issuer", Group: "cert-manager.io"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := CertManagerConfig()
			require.NotNil(t, b)
			got := b.WithIssuerRef(tt.input)
			require.Same(t, b, got, "WithIssuerRef must return receiver for chaining")
			require.NotNil(t, b.IssuerRef)
			assert.Equal(t, tt.expected.Name, b.IssuerRef.Name)
			assert.Equal(t, tt.expected.Kind, b.IssuerRef.Kind)
			assert.Equal(t, tt.expected.Group, b.IssuerRef.Group)
			if tt.chained {
				got2 := got.WithIssuerRef(cmmeta.IssuerReference{Name: "other"})
				assert.Same(t, got, got2)
				assert.Equal(t, "other", b.IssuerRef.Name)
			}
		})
	}
}

// TestCertManagerConfigApplyConfiguration_WithIssuerRef_nilReceiver documents that calling WithIssuerRef on nil receiver panics.
func TestCertManagerConfigApplyConfiguration_WithIssuerRef_nilReceiver(t *testing.T) {
	var b *CertManagerConfigApplyConfiguration
	panicked := false
	func() {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		_ = b.WithIssuerRef(cmmeta.IssuerReference{Name: "x"})
	}()
	assert.True(t, panicked, "calling WithIssuerRef on nil receiver must panic")
}
