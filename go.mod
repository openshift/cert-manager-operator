module github.com/openshift/cert-manager-operator

go 1.16

require (
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/google/go-jsonnet v0.17.0
	github.com/mogensen/kubernetes-split-yaml v0.3.0
	github.com/openshift/api v0.0.0-20211209135129-c58d9f695577
	github.com/openshift/build-machinery-go v0.0.0-20211213093930-7e33a7eb4ce3
	github.com/openshift/client-go v0.0.0-20211209144617-7385dd6338e3
	github.com/openshift/library-go v0.0.0-20220117173518-ca57b619b5d6
	github.com/spf13/cobra v1.2.1
	github.com/spf13/pflag v1.0.5
	golang.org/x/tools v0.1.6-0.20210820212750-d4cc65f0b2ff
	k8s.io/api v0.23.0
	k8s.io/apiextensions-apiserver v0.23.0
	k8s.io/apimachinery v0.23.0
	k8s.io/client-go v0.23.0
	k8s.io/code-generator v0.23.0
	k8s.io/component-base v0.23.0
)
