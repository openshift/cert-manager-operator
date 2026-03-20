package assets

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// Known asset from bindata (cert-manager-tokenrequest RoleBinding).
	tokenRequestRBAsset = "cert-manager-deployment/controller/cert-manager-tokenrequest-rb.yaml"
	// Known directory in the bintree.
	certManagerDeploymentDir = "cert-manager-deployment"
	controllerDir            = "cert-manager-deployment/controller"
)

// TestAsset provides table-driven tests for Asset(name string) ([]byte, error).
func TestAsset(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectedMin int
		expectError bool
		errorMsg    string
	}{
		{
			name:        "happy path - valid asset name returns bytes",
			input:       tokenRequestRBAsset,
			expectedMin: 50,
			expectError: false,
		},
		{
			name:        "happy path - backslash normalized to slash",
			input:       strings.ReplaceAll(tokenRequestRBAsset, "/", "\\"),
			expectedMin: 50,
			expectError: false,
		},
		{
			name:        "error case - empty name",
			input:       "",
			expectError: true,
			errorMsg:    "not found",
		},
		{
			name:        "error case - unknown asset",
			input:       "nonexistent/path.yaml",
			expectError: true,
			errorMsg:    "not found",
		},
		{
			name:        "error case - typo in asset name",
			input:       "cert-manager-deployment/controller/cert-manager-tokenrequest-rb.yam",
			expectError: true,
			errorMsg:    "not found",
		},
		{
			name:        "boundary - single known asset from AssetNames",
			input:       tokenRequestRBAsset,
			expectedMin: 1,
			expectError: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := Asset(tt.input)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, data)
			assert.GreaterOrEqual(t, len(data), tt.expectedMin, "asset content length")
		})
	}
}

// TestAssetNames ensures AssetNames() returns a non-empty list of known keys.
func TestAssetNames(t *testing.T) {
	tests := []struct {
		name          string
		expectEmpty   bool
		mustContain   string
		description   string
	}{
		{
			name:        "returns non-empty list",
			expectEmpty: false,
			mustContain: tokenRequestRBAsset,
			description: "AssetNames must include tokenrequest-rb asset",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			names := AssetNames()
			if tt.expectEmpty {
				assert.Empty(t, names)
				return
			}
			require.NotEmpty(t, names, "AssetNames() should not be empty")
			assert.Contains(t, names, tt.mustContain, tt.description)
		})
	}
}

// TestAssetDir provides table-driven tests for AssetDir(name string) ([]string, error).
func TestAssetDir(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		errorMsg    string
		minChildren int
		description string
	}{
		{
			name:        "happy path - root dir returns top-level children",
			input:       "",
			expectError: false,
			minChildren: 1,
			description: "empty name returns root children (e.g. cert-manager-deployment)",
		},
		{
			name:        "happy path - cert-manager-deployment dir",
			input:       certManagerDeploymentDir,
			expectError: false,
			minChildren: 1,
			description: "directory returns child names",
		},
		{
			name:        "happy path - controller subdir",
			input:       controllerDir,
			expectError: false,
			minChildren: 1,
			description: "controller dir has at least one child",
		},
		{
			name:        "error case - path to file not dir",
			input:       tokenRequestRBAsset,
			expectError: true,
			errorMsg:    "not found",
			description: "AssetDir on a file path returns error",
		},
		{
			name:        "error case - nonexistent path",
			input:       "nonexistent/dir",
			expectError: true,
			errorMsg:    "not found",
			description: "nonexistent path returns error",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			children, err := AssetDir(tt.input)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, children)
			assert.GreaterOrEqual(t, len(children), tt.minChildren, tt.description)
		})
	}
}

// TestAssetInfo provides table-driven tests for AssetInfo(name string) (os.FileInfo, error).
func TestAssetInfo(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		errorMsg    string
		checkInfo   func(t *testing.T, fi os.FileInfo)
	}{
		{
			name:        "happy path - valid asset returns FileInfo",
			input:       tokenRequestRBAsset,
			expectError: false,
			checkInfo: func(t *testing.T, fi os.FileInfo) {
				require.NotNil(t, fi)
				assert.False(t, fi.IsDir())
				assert.Equal(t, "cert-manager-deployment/controller/cert-manager-tokenrequest-rb.yaml", fi.Name())
			},
		},
		{
			name:        "error case - asset not found",
			input:       "not/found.yaml",
			expectError: true,
			errorMsg:    "not found",
		},
		{
			name:        "error case - empty name",
			input:       "",
			expectError: true,
			errorMsg:    "not found",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fi, err := AssetInfo(tt.input)
			if tt.expectError {
				require.Error(t, err)
				if tt.errorMsg != "" {
					assert.Contains(t, err.Error(), tt.errorMsg)
				}
				return
			}
			require.NoError(t, err)
			if tt.checkInfo != nil {
				tt.checkInfo(t, fi)
			}
		})
	}
}

// TestMustAsset verifies MustAsset panics on error and returns bytes on success.
func TestMustAsset(t *testing.T) {
	t.Run("happy path - valid name returns bytes", func(t *testing.T) {
		data := MustAsset(tokenRequestRBAsset)
		require.NotNil(t, data)
		assert.GreaterOrEqual(t, len(data), 50)
	})
	t.Run("panic on unknown asset", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r, "MustAsset must panic when asset not found")
			assert.Contains(t, r.(string), "not found")
		}()
		_ = MustAsset("nonexistent.yaml")
	})
	t.Run("panic on empty name", func(t *testing.T) {
		defer func() {
			r := recover()
			require.NotNil(t, r)
		}()
		_ = MustAsset("")
	})
}

// TestCertManagerDeploymentControllerCertManagerTokenrequestRbYaml verifies the tokenrequest-rb
// asset is loadable via Asset (the generator functions are package-private).
func TestCertManagerDeploymentControllerCertManagerTokenrequestRbYaml(t *testing.T) {
	data, err := Asset(tokenRequestRBAsset)
	require.NoError(t, err)
	require.NotNil(t, data)
	// Content must look like a RoleBinding (YAML with kind and apiVersion).
	str := string(data)
	assert.Contains(t, str, "RoleBinding")
	assert.Contains(t, str, "rbac.authorization.k8s.io")
}

// TestCertManagerDeploymentControllerCertManagerTokenrequestRbYamlBytes verifies the same
// content is returned by loading the asset (Bytes is internal; we test via Asset).
func TestCertManagerDeploymentControllerCertManagerTokenrequestRbYamlBytes(t *testing.T) {
	data, err := Asset(tokenRequestRBAsset)
	require.NoError(t, err)
	require.NotNil(t, data)
	// Bytes() is used by the internal *Yaml() function; Asset returns the same bytes.
	assert.Contains(t, string(data), "cert-manager-tokenrequest")
}
