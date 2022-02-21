package deployment

import (
	"os"
	"strings"
)

var imageEnvMap = map[string]string{
	"quay.io/jetstack/cert-manager-controller": "RELATED_IMAGE_CERT_MANAGER_CONTROLLER",
	"quay.io/jetstack/cert-manager-webhook":    "RELATED_IMAGE_CERT_MANAGER_WEBHOOK",
	"quay.io/jetstack/cert-manager-cainjector": "RELATED_IMAGE_CERT_MANAGER_CA_INJECTOR",
}

// Overrides provided image when envCertManagerControllerRelatedImage environment variable is specified.
// Otherwise, returns provided defaultImage.
//
// This function assumes the Cert Manager is provided in all-in-one image. This function will need to be updated
// is this assumption doesn't hold.
func certManagerImage(defaultImage string) string {
	for image, env := range imageEnvMap {
		if strings.Contains(defaultImage, image) {
			overriddenImage := os.Getenv(env)
			if overriddenImage != "" {
				return overriddenImage
			}
		}
	}
	return defaultImage
}
