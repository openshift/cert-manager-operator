FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.17-openshift-4.10 AS builder
WORKDIR /go/src/github.com/openshift/cert-manager-operator
COPY . .
RUN make build --warn-undefined-variables

FROM registry.ci.openshift.org/ocp/4.10:base
COPY --from=builder /go/src/github.com/openshift/cert-manager-operator/cert-manager-operator /usr/bin/
