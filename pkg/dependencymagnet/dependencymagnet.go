//go:build dependencymagnet
// +build dependencymagnet

// Package dependencymagnet is used to ensure packages required by build scripts
// but not directly imported by Go code are included in go.mod and vendored.
package dependencymagnet

import (
	// go-bindata is needed by bindata.mk for generating bindata
	_ "github.com/go-bindata/go-bindata/go-bindata"

	// build-machinery-go is needed for Makefile includes (bindata.mk)
	_ "github.com/openshift/build-machinery-go"

	// code-generator is needed by hack/update-clientgen.sh (kube_codegen.sh)
	_ "k8s.io/code-generator"
)
