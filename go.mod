module github.com/openshift/cert-manager-operator

go 1.16

require (
	github.com/go-bindata/go-bindata v3.1.2+incompatible
	github.com/mogensen/kubernetes-split-yaml v0.3.0
	github.com/openshift/api v0.0.0-20210706092853-b63d499a70ce
	github.com/openshift/build-machinery-go v0.0.0-20210614124016-792d61687197
	github.com/openshift/client-go v0.0.0-20210521082421-73d9475a9142
	github.com/openshift/library-go v0.0.0-20210715082010-d85b7751bff0
	github.com/spf13/cobra v1.1.3
	github.com/spf13/pflag v1.0.5
	golang.org/x/tools v0.1.3
	k8s.io/api v0.21.2
	k8s.io/apiextensions-apiserver v0.21.1
	k8s.io/apimachinery v0.21.2
	k8s.io/client-go v0.21.2
	k8s.io/code-generator v0.21.2
	k8s.io/component-base v0.21.2
)
