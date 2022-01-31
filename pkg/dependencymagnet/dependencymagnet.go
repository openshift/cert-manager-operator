//go:build dependencymagnet
// +build dependencymagnet

package tools

import (
	_ "github.com/go-bindata/go-bindata/go-bindata"
	_ "github.com/google/go-jsonnet/cmd/jsonnet"
	_ "github.com/mogensen/kubernetes-split-yaml"
	_ "github.com/openshift/build-machinery-go"
	_ "k8s.io/code-generator"
	_ "k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo"
)
