//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"text/template"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
)

// IssuerConfig customizes fields in the issuer spec
type IssuerConfig struct {
	GCPProjectID   string
	IBMCloudCISCRN string
}

// Certificate customize fields in the cert spec
type CertificateConfig struct {
	DNSName string
}

// IstioCSRGRPCurlJobConfig customizes the fields in a job spec
type IstioCSRGRPCurlJobConfig struct {
	CertificateSigningRequest string
	IstioCSRStatus            v1alpha1.IstioCSRStatus
	ClusterID                 string
	JobName                   string
	ProtoConfigMapName        string
	ServiceAccountName        string
}

// ServiceMonitorConfig customizes fields in the ServiceMonitor spec
type ServiceMonitorConfig struct {
	Name          string
	Namespace     string
	AppName       string
	ComponentName string
}

// OSSMv3Config customizes OpenShift Service Mesh v3 install manifests.
type OSSMv3Config struct {
	OperatorVersion string
	IstioVersion    string
	ClusterID       string
	CAAddress       string
}

const (
	istioCSRProfileMinimal  = "minimal"
	istioCSRProfileOSSM     = "ossm"
	istioCSROperandManifest = "testdata/istio/istio_csr_template.yaml"
)

// IstioCSRConfig customizes the IstioCSR operand manifest.
// Profile is "minimal" (default) for isolated IstioCSR tests or "ossm" for Service Mesh smoke.
// IstioNamespace is spec.istioCSRConfig.istio.namespace; for the minimal profile it must match
// the test namespace where the istio-ca Issuer is created.
type IstioCSRConfig struct {
	Namespace                       string
	IstioNamespace                  string
	ClusterID                       string
	IstioDataPlaneNamespaceSelector string
	Profile                         string
	IssuerName                      string
}

func istioCSRConfigForNS(namespace string, overrides IstioCSRConfig) IstioCSRConfig {
	if overrides.Namespace == "" {
		overrides.Namespace = namespace
	}
	if overrides.IstioNamespace == "" {
		overrides.IstioNamespace = namespace
	}
	if overrides.Profile == "" {
		overrides.Profile = istioCSRProfileMinimal
	}
	return overrides
}

// replaceWithTemplate puts field values from a template struct
func replaceWithTemplate(sourceFileContents string, templatedValues any) ([]byte, error) {
	tmpl, err := template.New("template").Option("missingkey=error").Parse(sourceFileContents)
	if err != nil {
		return nil, err
	}

	var doc bytes.Buffer
	err = tmpl.Execute(&doc, templatedValues)
	if err != nil {
		return nil, err
	}

	return doc.Bytes(), nil
}

// AssetFunc wraps the asset load function (used in dynamic resource loader),
// and extends it with a hook to allow template value replacement.
type AssetFunc func(name string) ([]byte, error)

// WithTemplateValues is a wrapper for using `replaceWithTemplate` with an `AssetFunc`,
// i.e. chains the loading -> modification.
func (sourceFn AssetFunc) WithTemplateValues(templatedValues any) AssetFunc {
	x := func(name string) ([]byte, error) {
		bytes, err := sourceFn(name)
		if err != nil {
			return nil, err
		}

		fileContentsStr := string(bytes)
		return replaceWithTemplate(fileContentsStr, templatedValues)
	}
	return x
}
