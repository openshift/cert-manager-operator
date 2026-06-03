FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.25-openshift-4.21 AS builder

ARG RELEASE_BRANCH=release-1.19

ARG GO_BUILD_TAGS=strictfipsruntime,openssl
ENV GOEXPERIMENT strictfipsruntime
ENV CGO_ENABLED 1

RUN mkdir -p /go/src/github.com/cert-manager
RUN git clone --depth 1 --branch $RELEASE_BRANCH https://github.com/openshift/cert-manager-trust-manager.git /go/src/github.com/cert-manager/trust-manager
WORKDIR /go/src/github.com/cert-manager/trust-manager

RUN go mod vendor
RUN go build -mod=vendor -tags $GO_BUILD_TAGS -ldflags '-w -s' -o /app/cert-manager-trust-manager ./cmd/trust-manager

FROM registry.access.redhat.com/ubi9-minimal:9.4
COPY --from=builder /app/cert-manager-trust-manager /usr/local/bin/cert-manager-trust-manager
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cert-manager-trust-manager"]
