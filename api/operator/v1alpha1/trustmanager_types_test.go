package v1alpha1

import (
	"os"
	"path"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/yaml"
)

const (
	trustmanagerCRDFile     = "operator.openshift.io_trustmanagers.yaml"
	trustmanagerCRDFilePath = "../../../config/crd/bases"
)

// TestTrustManagerStatusDefault verifies that the trustmanager CR status does not have default value.
// The admission code is expecting that the trustmanager status field will not have a default value.
// It allows separating between clean installation and the roll-back to the previous version of the cluster.
func TestTrustManagerStatusDefault(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})
	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		status, exists, err := unstructured.NestedMap(trustmanagerCRDVersion, "schema", "openAPIV3Schema", "properties", "status")
		if err != nil {
			t.Fatalf("failed to get nested map: %v", err)
		}

		if !exists {
			t.Fatalf("one of fields does not exist under the CRD")
		}

		if _, ok := status["default"]; ok {
			t.Fatalf("expected no default for the trustmanager CRD status")
		}
	}
}

// TestTrustManagerCRDSingletonValidation verifies that the CRD has the singleton XValidation rule.
func TestTrustManagerCRDSingletonValidation(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})
	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		schema, exists, err := unstructured.NestedMap(trustmanagerCRDVersion, "schema", "openAPIV3Schema")
		if err != nil {
			t.Fatalf("failed to get nested map: %v", err)
		}

		if !exists {
			t.Fatalf("openAPIV3Schema does not exist under the CRD")
		}

		validations, exists, err := unstructured.NestedSlice(schema, "x-kubernetes-validations")
		if err != nil {
			t.Fatalf("failed to get x-kubernetes-validations: %v", err)
		}

		if !exists || len(validations) == 0 {
			t.Fatalf("expected x-kubernetes-validations for singleton constraint")
		}

		found := false
		for _, val := range validations {
			valMap, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			rule, _ := valMap["rule"].(string)
			if rule == "self.metadata.name == 'cluster'" {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("expected singleton validation rule 'self.metadata.name == cluster'")
		}
	}
}

// TestTrustManagerCRDScope verifies that the CRD scope is Cluster.
func TestTrustManagerCRDScope(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	scope, ok := trustmanagerCRDSpec["scope"].(string)
	if !ok || scope != "Cluster" {
		t.Fatalf("expected CRD scope to be 'Cluster', got %q", scope)
	}
}

// TestTrustManagerCRDRequiredFields verifies that the CRD has spec as required and
// trustManagerConfig as required within spec.
func TestTrustManagerCRDRequiredFields(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})
	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})

		// Check spec is required at root level
		rootRequired, _, _ := unstructured.NestedStringSlice(trustmanagerCRDVersion, "schema", "openAPIV3Schema", "required")
		foundSpec := false
		for _, r := range rootRequired {
			if r == "spec" {
				foundSpec = true
				break
			}
		}
		if !foundSpec {
			t.Fatalf("expected 'spec' to be a required field at root level")
		}

		// Check trustManagerConfig is required within spec
		specRequired, _, _ := unstructured.NestedStringSlice(trustmanagerCRDVersion, "schema", "openAPIV3Schema", "properties", "spec", "required")
		foundTMConfig := false
		for _, r := range specRequired {
			if r == "trustManagerConfig" {
				foundTMConfig = true
				break
			}
		}
		if !foundTMConfig {
			t.Fatalf("expected 'trustManagerConfig' to be a required field within spec")
		}
	}
}

// TestTrustManagerCRDEnumFields verifies that enum fields have the correct allowed values.
func TestTrustManagerCRDEnumFields(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	tests := []struct {
		name         string
		path         []string
		expectedEnum []string
	}{
		{
			name:         "logFormat enum",
			path:         []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "logFormat", "enum"},
			expectedEnum: []string{"text", "json"},
		},
		{
			name:         "filterExpiredCertificates enum",
			path:         []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "filterExpiredCertificates", "enum"},
			expectedEnum: []string{"Enabled", "Disabled"},
		},
		{
			name:         "secretTargets.policy enum",
			path:         []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "secretTargets", "properties", "policy", "enum"},
			expectedEnum: []string{"Disabled", "Custom"},
		},
		{
			name:         "defaultCAPackage.policy enum",
			path:         []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "defaultCAPackage", "properties", "policy", "enum"},
			expectedEnum: []string{"Enabled", "Disabled"},
		},
	}

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				enumSlice, exists, err := unstructured.NestedSlice(trustmanagerCRDVersion, tt.path...)
				if err != nil {
					t.Fatalf("failed to get enum for %s: %v", tt.name, err)
				}
				if !exists {
					t.Fatalf("enum not found for %s", tt.name)
				}
				if len(enumSlice) != len(tt.expectedEnum) {
					t.Fatalf("expected %d enum values for %s, got %d", len(tt.expectedEnum), tt.name, len(enumSlice))
				}
				for i, expected := range tt.expectedEnum {
					actual, ok := enumSlice[i].(string)
					if !ok || actual != expected {
						t.Fatalf("expected enum value %q at index %d for %s, got %q", expected, i, tt.name, actual)
					}
				}
			})
		}
	}
}

// TestTrustManagerCRDDefaultValues verifies that default values are set correctly in the CRD.
func TestTrustManagerCRDDefaultValues(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	tests := []struct {
		name          string
		path          []string
		expectedValue interface{}
	}{
		{
			name:          "logLevel default",
			path:          []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "logLevel", "default"},
			expectedValue: float64(1),
		},
		{
			name:          "logFormat default",
			path:          []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "logFormat", "default"},
			expectedValue: "text",
		},
		{
			name:          "trustNamespace default",
			path:          []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "trustNamespace", "default"},
			expectedValue: "cert-manager",
		},
		{
			name:          "filterExpiredCertificates default",
			path:          []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "filterExpiredCertificates", "default"},
			expectedValue: "Disabled",
		},
		{
			name:          "secretTargets.policy default",
			path:          []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "secretTargets", "properties", "policy", "default"},
			expectedValue: "Disabled",
		},
		{
			name:          "defaultCAPackage.policy default",
			path:          []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "defaultCAPackage", "properties", "policy", "default"},
			expectedValue: "Disabled",
		},
	}

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				val, exists, err := unstructured.NestedFieldNoCopy(trustmanagerCRDVersion, tt.path...)
				if err != nil {
					t.Fatalf("failed to get default for %s: %v", tt.name, err)
				}
				if !exists {
					t.Fatalf("default not found for %s", tt.name)
				}
				if val != tt.expectedValue {
					t.Fatalf("expected default %v for %s, got %v", tt.expectedValue, tt.name, val)
				}
			})
		}
	}
}

// TestTrustManagerCRDLogLevelBounds verifies that logLevel has correct min/max bounds.
func TestTrustManagerCRDLogLevelBounds(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		basePath := []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "logLevel"}

		min, exists, _ := unstructured.NestedFieldNoCopy(trustmanagerCRDVersion, append(basePath, "minimum")...)
		if !exists {
			t.Fatalf("logLevel minimum not found")
		}
		if min != float64(1) {
			t.Fatalf("expected logLevel minimum to be 1, got %v", min)
		}

		max, exists, _ := unstructured.NestedFieldNoCopy(trustmanagerCRDVersion, append(basePath, "maximum")...)
		if !exists {
			t.Fatalf("logLevel maximum not found")
		}
		if max != float64(5) {
			t.Fatalf("expected logLevel maximum to be 5, got %v", max)
		}
	}
}

// TestTrustManagerCRDTrustNamespaceBounds verifies that trustNamespace has correct length constraints.
func TestTrustManagerCRDTrustNamespaceBounds(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		basePath := []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "trustNamespace"}

		minLen, exists, _ := unstructured.NestedFieldNoCopy(trustmanagerCRDVersion, append(basePath, "minLength")...)
		if !exists {
			t.Fatalf("trustNamespace minLength not found")
		}
		if minLen != float64(1) {
			t.Fatalf("expected trustNamespace minLength to be 1, got %v", minLen)
		}

		maxLen, exists, _ := unstructured.NestedFieldNoCopy(trustmanagerCRDVersion, append(basePath, "maxLength")...)
		if !exists {
			t.Fatalf("trustNamespace maxLength not found")
		}
		if maxLen != float64(63) {
			t.Fatalf("expected trustNamespace maxLength to be 63, got %v", maxLen)
		}
	}
}

// TestTrustManagerCRDSecretTargetsValidation verifies that the secretTargets field has proper CEL validation rules.
func TestTrustManagerCRDSecretTargetsValidation(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	expectedValidations := []string{
		"authorizedSecrets must not be empty when policy is Custom",
		"authorizedSecrets must be empty when policy is not Custom",
	}

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		validationsPath := []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "secretTargets", "x-kubernetes-validations"}

		validations, exists, err := unstructured.NestedSlice(trustmanagerCRDVersion, validationsPath...)
		if err != nil {
			t.Fatalf("failed to get secretTargets validations: %v", err)
		}
		if !exists {
			t.Fatalf("secretTargets x-kubernetes-validations not found")
		}

		for _, expectedMsg := range expectedValidations {
			found := false
			for _, val := range validations {
				valMap, ok := val.(map[string]interface{})
				if !ok {
					continue
				}
				msg, _ := valMap["message"].(string)
				if msg == expectedMsg {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected secretTargets validation with message %q", expectedMsg)
			}
		}
	}
}

// TestTrustManagerCRDTrustNamespaceImmutability verifies that the trustNamespace field has immutability validation.
func TestTrustManagerCRDTrustNamespaceImmutability(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		validationsPath := []string{"schema", "openAPIV3Schema", "properties", "spec", "properties", "trustManagerConfig", "properties", "trustNamespace", "x-kubernetes-validations"}

		validations, exists, err := unstructured.NestedSlice(trustmanagerCRDVersion, validationsPath...)
		if err != nil {
			t.Fatalf("failed to get trustNamespace validations: %v", err)
		}
		if !exists {
			t.Fatalf("trustNamespace x-kubernetes-validations not found")
		}

		found := false
		for _, val := range validations {
			valMap, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			msg, _ := valMap["message"].(string)
			if msg == "trustNamespace is immutable once set" {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected trustNamespace immutability validation rule")
		}
	}
}

// TestTrustManagerCRDPrinterColumns verifies that the CRD has the correct additional printer columns.
func TestTrustManagerCRDPrinterColumns(t *testing.T) {
	filepath := path.Join(trustmanagerCRDFilePath, trustmanagerCRDFile)
	trustmanagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read trustmanager CRD file %q: %v", filepath, err)
	}

	var trustmanagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustmanagerCRDBytes, &trustmanagerCRD); err != nil {
		t.Fatalf("failed to unmarshal trustmanager CRD: %v", err)
	}
	trustmanagerCRDSpec := trustmanagerCRD["spec"].(map[string]interface{})
	trustmanagerCRDVersions := trustmanagerCRDSpec["versions"].([]interface{})

	expectedColumns := []struct {
		name     string
		jsonPath string
	}{
		{name: "Ready", jsonPath: ".status.conditions[?(@.type=='Ready')].status"},
		{name: "Message", jsonPath: ".status.conditions[?(@.type=='Ready')].message"},
		{name: "AGE", jsonPath: ".metadata.creationTimestamp"},
	}

	for _, v := range trustmanagerCRDVersions {
		trustmanagerCRDVersion := v.(map[string]interface{})
		columns, exists := trustmanagerCRDVersion["additionalPrinterColumns"]
		if !exists {
			t.Fatalf("additionalPrinterColumns not found")
		}

		columnList, ok := columns.([]interface{})
		if !ok {
			t.Fatalf("additionalPrinterColumns is not a list")
		}

		if len(columnList) != len(expectedColumns) {
			t.Fatalf("expected %d printer columns, got %d", len(expectedColumns), len(columnList))
		}

		for i, expected := range expectedColumns {
			col, ok := columnList[i].(map[string]interface{})
			if !ok {
				t.Fatalf("printer column at index %d is not a map", i)
			}
			if col["name"] != expected.name {
				t.Fatalf("expected printer column name %q at index %d, got %q", expected.name, i, col["name"])
			}
			if col["jsonPath"] != expected.jsonPath {
				t.Fatalf("expected printer column jsonPath %q at index %d, got %q", expected.jsonPath, i, col["jsonPath"])
			}
		}
	}
}
