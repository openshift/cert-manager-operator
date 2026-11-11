FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.26-rhel9 AS builder
WORKDIR /go/src/github.com/openshift/cert-manager-operator

# build operator
COPY . .
RUN make build --warn-undefined-variables

FROM registry.redhat.io/ubi9/ubi-minimal-pqc:latest
COPY --from=builder /go/src/github.com/openshift/cert-manager-operator/cert-manager-operator /usr/bin/

USER 65532:65532

ENTRYPOINT ["/usr/bin/cert-manager-operator"]
