package apis

import (
	"context"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

/*
 * Content taken from https://github.com/openshift/api/tree/master/tests
 */

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var testScheme *runtime.Scheme
var ctx = context.Background()
var suites []SuiteSpec

// SuiteSpec defines a test suite specification.
type SuiteSpec struct {
	// Name is the name of the test suite.
	Name string `json:"name"`

	CRDName string `json:"crdName"`

	// Version is the version of the CRD under test in this file.
	// When omitted, if there is a single version in the CRD, this is assumed to be the correct version.
	// If there are multiple versions within the CRD, an educated guess is made based on the directory structure.
	Version string `json:"version,omitempty"`

	// Tests defines the test cases to run for this test suite.
	Tests TestSpec `json:"tests"`

	// PerTestRuntimeInfo cannot be specified in the testcase itself, but at runtime must be computed.
	PerTestRuntimeInfo *PerTestRuntimeInfo `json:"-"`
}

// TestSpec defines the test specs for individual tests in this suite.
type TestSpec struct {
	// OnCreate defines a list of on create style tests.
	OnCreate []OnCreateTestSpec `json:"onCreate"`

	// OnUpdate defines a list of on create style tests.
	OnUpdate []OnUpdateTestSpec `json:"onUpdate"`
}

// OnCreateTestSpec defines an individual test case for the on create style tests.
type OnCreateTestSpec struct {
	// Name is the name of this test case.
	Name string `json:"name"`

	// ResourceName is the name to be used for the resource under test.
	ResourceName string `json:"resourceName"`

	// UseGenerateName is for indicating whether random string should be prefixed to the ResourceName.
	UseGenerateName bool `json:"useGenerateName"`

	// Initial is a literal string containing the initial YAML content from which to
	// create the resource.
	// Note `apiVersion` and `kind` fields are required though `metadata` can be omitted.
	// Typically, this will vary in `spec` only test to test.
	Initial string `json:"initial"`

	// ExpectedError defines the error string that should be returned when the initial resourec is invalid.
	// This will be matched as a substring of the actual error when non-empty.
	ExpectedError string `json:"expectedError"`

	// Expected is a literal string containing the expected YAML content that should be
	// persisted when the resource is created.
	// Note `apiVersion` and `kind` fields are required though `metadata` can be omitted.
	// Typically, this will vary in `spec` only test to test.
	Expected string `json:"expected"`
}

type PerTestRuntimeInfo struct {
	// CRDFilenames indicates all the CRD filenames that this test applies to.  Remember that tests can apply to multiple
	// files depending on whether their gates are included in each one.
	CRDFilenames []string `json:"-"`
}

// OnUpdateTestSpec defines an individual test case for the on update style tests.
type OnUpdateTestSpec struct {
	// Name is the name of this test case.
	Name string `json:"name"`

	// ResourceName is the name to be used for the resource under test.
	ResourceName string `json:"resourceName"`

	// UseGenerateName is for indicating whether random string should be prefixed to the ResourceName.
	UseGenerateName bool `json:"useGenerateName"`

	// InitialCRDPatches is a list of YAML patches to apply to the CRD before applying
	// the initial version of the resource.
	// Once the initial version has been applied, the CRD will be restored to its
	// original state before the updated object is applied.
	// This can be used to test ratcheting validation of CRD schema changes over time.
	InitialCRDPatches []Patch `json:"initialCRDPatches"`

	// Initial is a literal string containing the initial YAML content from which to
	// create the resource.
	// Note `apiVersion` and `kind` fields are required though `metadata` can be omitted.
	// Typically, this will vary in `spec` only test to test.
	Initial string `json:"initial"`

	// Updated is a literal string containing the updated YAML content from which to
	// update the resource.
	// Note `apiVersion` and `kind` fields are required though `metadata` can be omitted.
	// Typically, this will vary in `spec` only test to test.
	Updated string `json:"updated"`

	// ExpectedError defines the error string that should be returned when the updated resource is invalid.
	// This will be matched as a substring of the actual error when non-empty.
	ExpectedError string `json:"expectedError"`

	// ExpectedStatusError defines the error string that should be returned when the updated resource status is invalid.
	// This will be matched as a substring of the actual error when non-empty.
	ExpectedStatusError string `json:"expectedStatusError"`

	// Expected is a literal string containing the expected YAML content that should be
	// persisted when the resource is updated.
	// Note `apiVersion` and `kind` fields are required though `metadata` can be omitted.
	// Typically, this will vary in `spec` only test to test.
	Expected string `json:"expected"`
}

// Patch represents a single operation to be applied to a YAML document.
// It follows the JSON Patch format as defined in RFC 6902.
// Each patch operation is atomic and can be used to modify the structure
// or content of a YAML document.
type Patch struct {
	// Op is the operation to be performed. Common operations include "add", "remove", "replace", "move", "copy", and "test".
	Op string `json:"op"`

	// Path is a JSON Pointer that indicates the location in the YAML document where the operation is to be performed.
	Path string `json:"path"`

	// Value is the value to be used within the operation. This field is required for operations like "add" and "replace".
	Value *any `json:"value"`
}
