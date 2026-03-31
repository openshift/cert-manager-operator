#!/bin/bash

FAILS=false
for f in `find . -name "*crd.yaml" -type f`
do
    if [[ "$(./bin/yq e '.apiVersion' "$f")" == "apiextensions.k8s.io/v1" ]]; then
        v1beta1CRDName="$(./bin/yq e '.metadata.name' "$f")"
        if [[ "$(./bin/yq e '.spec.validation.openAPIV3Schema.properties.metadata.description' "$f")" != "null" ]]; then
            echo "Error: cannot have a metadata description in $f"
            FAILS=true
        fi

        if [[ "$(./bin/yq e '.spec.preserveUnknownFields' "$f")" != "false" ]]; then
            echo "Error: pruning not enabled (.spec.preserveUnknownFields != false) in $f"
            FAILS=true
        fi
    fi
done

if [ "$FAILS" = true ] ; then
    exit 1
fi

