#!/bin/bash

source "$(dirname "${BASH_SOURCE}")/lib/init.sh"

SCRIPT_ROOT=$(git rev-parse --show-toplevel)

if [[ "$(protoc --version)" != "libprotoc 3."* ]]; then
  echo "Generating protobuf requires protoc 3.0.x. Please download and
install the platform appropriate Protobuf package for your OS:

  https://github.com/google/protobuf/releases

To skip protobuf generation, set \$PROTO_OPTIONAL."
  exit 1
fi

rm -rf go-to-protobuf
rm -rf protoc-gen-gogo

# Build from root to use workspace vendor
mkdir -p _output/bin
go build -mod=vendor -o _output/bin/go-to-protobuf k8s.io/code-generator/cmd/go-to-protobuf
go build -mod=vendor -o _output/bin/protoc-gen-gogo k8s.io/code-generator/cmd/go-to-protobuf/protoc-gen-gogo

PATH="$PATH:_output/bin" go-to-protobuf \
  --output-dir="${GOPATH}/src" \
  --apimachinery-packages='-k8s.io/apimachinery/pkg/util/intstr,-k8s.io/apimachinery/pkg/api/resource,-k8s.io/apimachinery/pkg/runtime/schema,-k8s.io/apimachinery/pkg/runtime,-k8s.io/apimachinery/pkg/apis/meta/v1,-k8s.io/apimachinery/pkg/apis/meta/v1beta1,-k8s.io/api/core/v1,-k8s.io/api/rbac/v1' \
  --go-header-file=${SCRIPT_ROOT}/hack/empty.txt \
  --proto-import=${SCRIPT_ROOT}/third_party/protobuf \
  --proto-import=${SCRIPT_ROOT}/vendor \
  --packages="${API_PACKAGES}"
