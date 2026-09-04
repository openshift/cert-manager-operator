package certmanager

import (
	"testing"
)

func Test_certManagerImage(t *testing.T) {
	tests := []struct {
		name         string
		envVar       string
		defaultImage string
		envVarValue  string
		want         string
	}{
		{
			name:         "controller: use default on empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_CONTROLLER",
			defaultImage: testUpstreamCertManagerControllerImage,
			envVarValue:  "",
			want:         testUpstreamCertManagerControllerImage,
		},
		{
			name:         "controller: use override on non-empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_CONTROLLER",
			defaultImage: testUpstreamCertManagerControllerImage,
			envVarValue:  "registry.redhat.io/cert-manager/cert-manager-operator-1.5-rhel-8:latest",
			want:         "registry.redhat.io/cert-manager/cert-manager-operator-1.5-rhel-8:latest",
		},
		{
			name:         "webhook: use default on empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_WEBHOOK",
			defaultImage: "quay.io/jetstack/cert-manager-webhook:latest",
			envVarValue:  "",
			want:         "quay.io/jetstack/cert-manager-webhook:latest",
		},
		{
			name:         "webhook: use override on non-empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_WEBHOOK",
			defaultImage: "quay.io/jetstack/cert-manager-webhook:latest",
			envVarValue:  "registry.redhat.io/cert-manager/cert-manager-webhook-rhel-8:latest",
			want:         "registry.redhat.io/cert-manager/cert-manager-webhook-rhel-8:latest",
		},
		{
			name:         "cainjector: use default on empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_CA_INJECTOR",
			defaultImage: "quay.io/jetstack/cert-manager-cainjector:latest",
			envVarValue:  "",
			want:         "quay.io/jetstack/cert-manager-cainjector:latest",
		},
		{
			name:         "cainjector: use override on non-empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_CA_INJECTOR",
			defaultImage: "quay.io/jetstack/cert-manager-cainjector:latest",
			envVarValue:  "registry.redhat.io/cert-manager/cert-manager-cainjector-rhel-8:latest",
			want:         "registry.redhat.io/cert-manager/cert-manager-cainjector-rhel-8:latest",
		},
		{
			name:         "acmesolver: use default on empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_ACMESOLVER",
			defaultImage: "quay.io/jetstack/cert-manager-acmesolver:latest",
			envVarValue:  "",
			want:         "quay.io/jetstack/cert-manager-acmesolver:latest",
		},
		{
			name:         "acmesolver: use override on non-empty env var",
			envVar:       "RELATED_IMAGE_CERT_MANAGER_ACMESOLVER",
			defaultImage: "quay.io/jetstack/cert-manager-acmesolver:latest",
			envVarValue:  "registry.redhat.io/cert-manager/cert-manager-acmesolver-rhel-8:latest",
			want:         "registry.redhat.io/cert-manager/cert-manager-acmesolver-rhel-8:latest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(tt.envVar, tt.envVarValue)
			if got := certManagerImage(tt.defaultImage); got != tt.want {
				t.Errorf("certManagerImage() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_certManagerImageUnknownImage(t *testing.T) {
	t.Setenv("RELATED_IMAGE_CERT_MANAGER_CONTROLLER", "override:latest")

	defaultImage := "quay.io/some-other/image:v1.0"
	got := certManagerImage(defaultImage)
	if got != defaultImage {
		t.Errorf("certManagerImage() = %v, want %v", got, defaultImage)
	}
}
