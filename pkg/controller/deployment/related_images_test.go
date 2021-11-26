package deployment

import (
	"os"
	"testing"
)

func Test_certManagerImage(t *testing.T) {
	type args struct {
		defaultImage       string
		relatedImageEnvVar string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "Use default image on empty " + environmentalVariableCertManagerRelatedImage + " variable",
			args: args{
				defaultImage:       "quay.io/jetstack/cert-manager-controller:latest",
				relatedImageEnvVar: "",
			},
			want: "quay.io/jetstack/cert-manager-controller:latest",
		},
		{
			name: "Use related image on non-empty " + environmentalVariableCertManagerRelatedImage + " variable",
			args: args{
				defaultImage:       "quay.io/jetstack/cert-manager-controller:latest",
				relatedImageEnvVar: "registry.redhat.io/cert-manager/cert-manager-operator-1.5-rhel-8:latest",
			},
			want: "registry.redhat.io/cert-manager/cert-manager-operator-1.5-rhel-8:latest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv(environmentalVariableCertManagerRelatedImage, tt.args.relatedImageEnvVar)
			if got := certManagerImage(tt.args.defaultImage); got != tt.want {
				t.Errorf("certManagerImage() = %v, want %v", got, tt.want)
			}
			os.Unsetenv(environmentalVariableCertManagerRelatedImage)
		})
	}
}
