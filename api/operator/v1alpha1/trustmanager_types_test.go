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

// TestTrustManagerStatusDefault verifies that the trustmanager CR status does not have default value
// The admission code is expecting that the trustmanager status
// field will not have a default value.
// It allows separating between clean installation and the roll-back to the previous version of the cluster
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

// TestTrustManagerCRDSingleton verifies that the trustmanager CRD has the singleton validation rule
func TestTrustManagerCRDSingleton(t *testing.T) {
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
			t.Fatalf("openAPIV3Schema does not exist under the CRD version")
		}

		// Check for x-kubernetes-validations rules containing the singleton rule
		validations, exists, err := unstructured.NestedSlice(schema, "x-kubernetes-validations")
		if err != nil {
			t.Fatalf("failed to get x-kubernetes-validations: %v", err)
		}
		if !exists {
			t.Fatalf("x-kubernetes-validations does not exist on TrustManager CRD schema")
		}

		found := false
		for _, v := range validations {
			rule, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			if ruleStr, ok := rule["rule"].(string); ok {
				if ruleStr == "self.metadata.name == 'cluster'" {
					found = true
					break
				}
			}
		}
		if !found {
			t.Fatalf("expected singleton validation rule 'self.metadata.name == cluster' not found")
		}
	}
}

// TestTrustManagerCRDScope verifies that the trustmanager CRD is cluster-scoped
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
	if !ok {
		t.Fatalf("scope field not found in CRD spec")
	}
	if scope != "Cluster" {
		t.Fatalf("expected CRD scope to be 'Cluster', got %q", scope)
	}
}

// TestTrustManagerCRDTrustNamespaceImmutability verifies that trustNamespace has an immutability XValidation rule
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
		trustNamespace, exists, err := unstructured.NestedMap(trustmanagerCRDVersion,
			"schema", "openAPIV3Schema", "properties", "spec", "properties",
			"trustManagerConfig", "properties", "trustNamespace")
		if err != nil {
			t.Fatalf("failed to get trustNamespace: %v", err)
		}
		if !exists {
			t.Fatalf("trustNamespace field does not exist in the CRD")
		}

		validations, exists, err := unstructured.NestedSlice(trustNamespace, "x-kubernetes-validations")
		if err != nil {
			t.Fatalf("failed to get x-kubernetes-validations on trustNamespace: %v", err)
		}
		if !exists {
			t.Fatalf("trustNamespace does not have x-kubernetes-validations")
		}

		found := false
		for _, val := range validations {
			rule, ok := val.(map[string]interface{})
			if !ok {
				continue
			}
			if msg, ok := rule["message"].(string); ok {
				if msg == "trustNamespace is immutable once set" {
					found = true
					break
				}
			}
		}
		if !found {
			t.Fatalf("expected immutability validation rule for trustNamespace not found")
		}
	}
}
