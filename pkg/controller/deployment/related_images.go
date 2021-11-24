package deployment

import "os"

const (
	environmentalVariableCertManagerRelatedImage = "RELATED_IMAGE_CERT_MANAGER"
)

// Overrides provided image when environmentalVariableCertManagerRelatedImage environment variable is specified.
// Otherwise, returns provided defaultImage.
//
// This function assumes the Cert Manager is provided in all-in-one image. This function will need to be updated
// is this assumption doesn't hold.
func certManagerImage(defaultImage string) string {
	env := os.Getenv(environmentalVariableCertManagerRelatedImage)
	if env == "" {
		return defaultImage
	}
	return env
}
