//go:build e2e
// +build e2e

package e2e

import (
	"bytes"
	"text/template"
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

// replaceWithTemplate puts field values from a template struct
func replaceWithTemplate(sourceFileContents string, templatedValues any) ([]byte, error) {
	tmpl, err := template.New("template").Parse(sourceFileContents)
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
