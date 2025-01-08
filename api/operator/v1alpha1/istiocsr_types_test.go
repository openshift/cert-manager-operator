package v1alpha1

import (
	"os"
	"path"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"sigs.k8s.io/yaml"
)

const (
	istiocsrCRDFile     = "operator.openshift.io_istiocsrs.yaml"
	istiocsrCRDFilePath = "../../../config/crd/bases"
)

// TestIstioCSRStatusDefault verifies that the istiocsr CR status does not have default value
// The admission code under https://github.com/openshift/kubernetes/pull/877 is expecting that the istiocsr status
// field will not have a default value.
// It allows separating between clean installation and the roll-back to the previous version of the cluster
func TestIstioCSRStatusDefault(t *testing.T) {
	filepath := path.Join(istiocsrCRDFilePath, istiocsrCRDFile)
	istiocsrCRDBytes, err := os.ReadFile(filepath)
	if err != nil {
		t.Fatalf("failed to read istiocsr CRD file %q: %v", filepath, err)
	}

	var istiocsrCRD map[string]interface{}
	if err := yaml.Unmarshal(istiocsrCRDBytes, &istiocsrCRD); err != nil {
		t.Fatalf("failed to unmarshal istiocsr CRD: %v", err)
	}
	istiocsrCRDSpec := istiocsrCRD["spec"].(map[string]interface{})
	istiocsrCRDVersions := istiocsrCRDSpec["versions"].([]interface{})
	for _, v := range istiocsrCRDVersions {
		istiocsrCRDVersion := v.(map[string]interface{})
		status, exists, err := unstructured.NestedMap(istiocsrCRDVersion, "schema", "openAPIV3Schema", "properties", "status")
		if err != nil {
			t.Fatalf("failed to get nested map: %v", err)
		}

		if !exists {
			t.Fatalf("one of fields does not exist under the CRD")
		}

		if _, ok := status["default"]; ok {
			t.Fatalf("expected no default for the istiocsr CRD status")
		}
	}
}
