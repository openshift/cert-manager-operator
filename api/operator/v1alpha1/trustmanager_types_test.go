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
// The admission code under https://github.com/openshift/kubernetes/pull/877 is expecting that the trustmanager status
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
