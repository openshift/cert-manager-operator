//go:build tools
// +build tools

/*
Copyright 2020 The Kubernetes Authors.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// This package contains import references to packages required only for the
// build process.
// https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
package tools

import (
	// golangci-lint is used for linting the go code
	_ "github.com/golangci/golangci-lint/v2/cmd/golangci-lint"

	// go-bindata embeds static assets into Go binaries (used by bindata.mk)
	// Note: Using +incompatible version (not v3 module) for compatibility with bindata.mk
	_ "github.com/go-bindata/go-bindata/go-bindata"

	// jsonnet is used for templating cert-manager manifests (update-cert-manager-manifests.sh)
	_ "github.com/google/go-jsonnet/cmd/jsonnet"

	// counterfeiter generates test fakes/mocks for interfaces
	_ "github.com/maxbrunsfeld/counterfeiter/v6"

	// openshift/api/openapi is used for OpenAPI spec generation
	_ "github.com/openshift/api/openapi"

	// build-machinery-go provides Makefile includes (bindata.mk, targets)
	_ "github.com/openshift/build-machinery-go"

	// go-to-protobuf generates protobuf definitions from Go types
	_ "k8s.io/code-generator/cmd/go-to-protobuf"

	// protoc-gen-gogo is a protobuf compiler plugin for gogo/protobuf
	_ "k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo"

	// setup-envtest downloads and configures envtest binaries for controller tests
	_ "sigs.k8s.io/controller-runtime/tools/setup-envtest"

	// controller-gen generates CRDs, RBAC, and webhook manifests from Go types
	_ "sigs.k8s.io/controller-tools/cmd/controller-gen"

	// kustomize is used for building and customizing Kubernetes manifests
	_ "sigs.k8s.io/kustomize/kustomize/v5"

	// govulncheck is used for scanning the vulnerabilities in the used go packages
	_ "golang.org/x/vuln/cmd/govulncheck"
)
