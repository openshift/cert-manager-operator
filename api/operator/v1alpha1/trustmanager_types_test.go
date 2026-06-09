package v1alpha1

import (
	"os"
	"path"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/yaml"
)

const (
	trustManagerCRDFile     = "operator.openshift.io_trustmanagers.yaml"
	trustManagerCRDFilePath = "../../../config/crd/bases"
)

// TestTrustManagerStatusDefault verifies that the TrustManager CR status does not have default value.
// The admission code expects that the TrustManager status field will not have a default value.
// It allows separating between clean installation and the roll-back to the previous version of the cluster.
func TestTrustManagerStatusDefault(t *testing.T) {
	filepath := path.Join(trustManagerCRDFilePath, trustManagerCRDFile)
	trustManagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read TrustManager CRD file %q: %v", filepath, err)
	}

	var trustManagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustManagerCRDBytes, &trustManagerCRD); err != nil {
		t.Fatalf("failed to unmarshal TrustManager CRD: %v", err)
	}
	trustManagerCRDSpec := trustManagerCRD["spec"].(map[string]interface{})
	trustManagerCRDVersions := trustManagerCRDSpec["versions"].([]interface{})
	for _, v := range trustManagerCRDVersions {
		trustManagerCRDVersion := v.(map[string]interface{})
		status, exists, err := unstructured.NestedMap(trustManagerCRDVersion, "schema", "openAPIV3Schema", "properties", "status")
		if err != nil {
			t.Fatalf("failed to get nested map: %v", err)
		}

		if !exists {
			t.Fatalf("one of fields does not exist under the CRD")
		}

		if _, ok := status["default"]; ok {
			t.Fatalf("expected no default for the TrustManager CRD status")
		}
	}
}

// TestTrustManagerSingletonValidation verifies that the CRD has singleton validation rule.
func TestTrustManagerSingletonValidation(t *testing.T) {
	filepath := path.Join(trustManagerCRDFilePath, trustManagerCRDFile)
	trustManagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read TrustManager CRD file %q: %v", filepath, err)
	}

	var trustManagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustManagerCRDBytes, &trustManagerCRD); err != nil {
		t.Fatalf("failed to unmarshal TrustManager CRD: %v", err)
	}
	trustManagerCRDSpec := trustManagerCRD["spec"].(map[string]interface{})
	trustManagerCRDVersions := trustManagerCRDSpec["versions"].([]interface{})
	for _, v := range trustManagerCRDVersions {
		trustManagerCRDVersion := v.(map[string]interface{})
		schema, exists, err := unstructured.NestedMap(trustManagerCRDVersion, "schema", "openAPIV3Schema")
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

		if !exists {
			t.Fatalf("expected x-kubernetes-validations for singleton enforcement")
		}

		found := false
		for _, val := range validations {
			v, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			rule, _ := v["rule"].(string)
			if rule == "self.metadata.name == 'cluster'" {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("expected singleton validation rule 'self.metadata.name == cluster' not found")
		}
	}
}

// TestTrustManagerSpecRequired verifies that the spec field is required in the CRD.
func TestTrustManagerSpecRequired(t *testing.T) {
	filepath := path.Join(trustManagerCRDFilePath, trustManagerCRDFile)
	trustManagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read TrustManager CRD file %q: %v", filepath, err)
	}

	var trustManagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustManagerCRDBytes, &trustManagerCRD); err != nil {
		t.Fatalf("failed to unmarshal TrustManager CRD: %v", err)
	}
	trustManagerCRDSpec := trustManagerCRD["spec"].(map[string]interface{})
	trustManagerCRDVersions := trustManagerCRDSpec["versions"].([]interface{})
	for _, v := range trustManagerCRDVersions {
		trustManagerCRDVersion := v.(map[string]interface{})
		schema, exists, err := unstructured.NestedMap(trustManagerCRDVersion, "schema", "openAPIV3Schema")
		if err != nil {
			t.Fatalf("failed to get nested map: %v", err)
		}

		if !exists {
			t.Fatalf("openAPIV3Schema does not exist under the CRD")
		}

		required, exists, err := unstructured.NestedStringSlice(schema, "required")
		if err != nil {
			t.Fatalf("failed to get required fields: %v", err)
		}

		if !exists {
			t.Fatalf("expected required fields at root level")
		}

		found := false
		for _, r := range required {
			if r == "spec" {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("expected 'spec' to be in the required fields list")
		}
	}
}

// TestTrustManagerTrustNamespaceImmutability verifies that the trustNamespace field has immutability validation.
func TestTrustManagerTrustNamespaceImmutability(t *testing.T) {
	filepath := path.Join(trustManagerCRDFilePath, trustManagerCRDFile)
	trustManagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read TrustManager CRD file %q: %v", filepath, err)
	}

	var trustManagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustManagerCRDBytes, &trustManagerCRD); err != nil {
		t.Fatalf("failed to unmarshal TrustManager CRD: %v", err)
	}
	trustManagerCRDSpec := trustManagerCRD["spec"].(map[string]interface{})
	trustManagerCRDVersions := trustManagerCRDSpec["versions"].([]interface{})
	for _, v := range trustManagerCRDVersions {
		trustManagerCRDVersion := v.(map[string]interface{})
		trustNamespace, exists, err := unstructured.NestedMap(trustManagerCRDVersion,
			"schema", "openAPIV3Schema", "properties", "spec", "properties",
			"trustManagerConfig", "properties", "trustNamespace")
		if err != nil {
			t.Fatalf("failed to get trustNamespace: %v", err)
		}

		if !exists {
			t.Fatalf("trustNamespace field does not exist in schema")
		}

		validations, exists, err := unstructured.NestedSlice(trustNamespace, "x-kubernetes-validations")
		if err != nil {
			t.Fatalf("failed to get x-kubernetes-validations: %v", err)
		}

		if !exists {
			t.Fatalf("expected x-kubernetes-validations for trustNamespace immutability")
		}

		found := false
		for _, val := range validations {
			v, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			message, _ := v["message"].(string)
			if message == "trustNamespace is immutable once set" {
				found = true
				break
			}
		}

		if !found {
			t.Fatalf("expected immutability validation rule for trustNamespace not found")
		}
	}
}

// TestTrustManagerSecretTargetsValidation verifies that the secretTargets field has cross-field validation.
func TestTrustManagerSecretTargetsValidation(t *testing.T) {
	filepath := path.Join(trustManagerCRDFilePath, trustManagerCRDFile)
	trustManagerCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read TrustManager CRD file %q: %v", filepath, err)
	}

	var trustManagerCRD map[string]interface{}
	if err := yaml.Unmarshal(trustManagerCRDBytes, &trustManagerCRD); err != nil {
		t.Fatalf("failed to unmarshal TrustManager CRD: %v", err)
	}
	trustManagerCRDSpec := trustManagerCRD["spec"].(map[string]interface{})
	trustManagerCRDVersions := trustManagerCRDSpec["versions"].([]interface{})
	for _, v := range trustManagerCRDVersions {
		trustManagerCRDVersion := v.(map[string]interface{})
		secretTargets, exists, err := unstructured.NestedMap(trustManagerCRDVersion,
			"schema", "openAPIV3Schema", "properties", "spec", "properties",
			"trustManagerConfig", "properties", "secretTargets")
		if err != nil {
			t.Fatalf("failed to get secretTargets: %v", err)
		}

		if !exists {
			t.Fatalf("secretTargets field does not exist in schema")
		}

		validations, exists, err := unstructured.NestedSlice(secretTargets, "x-kubernetes-validations")
		if err != nil {
			t.Fatalf("failed to get x-kubernetes-validations: %v", err)
		}

		if !exists {
			t.Fatalf("expected x-kubernetes-validations for secretTargets")
		}

		expectedMessages := map[string]bool{
			"authorizedSecrets must not be empty when policy is Custom": false,
			"authorizedSecrets must be empty when policy is not Custom": false,
		}

		for _, val := range validations {
			v, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			message, _ := v["message"].(string)
			if _, exists := expectedMessages[message]; exists {
				expectedMessages[message] = true
			}
		}

		for msg, found := range expectedMessages {
			if !found {
				t.Errorf("expected validation rule with message %q not found", msg)
			}
		}
	}
}
