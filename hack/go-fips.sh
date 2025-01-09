#!/bin/bash

if GOEXPERIMENT="strictfipsruntime" go build ./tools; then
    echo "INFO: building with FIPS support"

    export GOEXPERIMENT="strictfipsruntime"
    export GOFLAGS="${GOFLAGS} -tags=strictfipsruntime,openssl"
else
    echo "WARN: building without FIPS support, GOEXPERIMENT strictfipsruntime is not available in the go compiler"
    echo "WARN: this build cannot be used in CI or production, due to lack of FIPS!!"
fi
