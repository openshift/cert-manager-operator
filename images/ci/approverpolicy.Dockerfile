FROM registry.redhat.io/openshift/golang-builder:golang-builder-v1.26-rhel9 AS builder

ARG RELEASE_BRANCH=v0.29.0

ARG GO_BUILD_TAGS=strictfipsruntime,openssl
ENV GOEXPERIMENT strictfipsruntime
ENV CGO_ENABLED 1

RUN mkdir -p /go/src/github.com/cert-manager
RUN git clone --depth 1 --branch $RELEASE_BRANCH https://github.com/openshift/cert-manager-approver-policy.git /go/src/github.com/cert-manager/approver-policy
WORKDIR /go/src/github.com/cert-manager/approver-policy

RUN go mod vendor
RUN go build -mod=vendor -tags $GO_BUILD_TAGS -ldflags '-w -s' -o /app/cert-manager-approver-policy ./cmd/

FROM registry.redhat.io/ubi9/ubi-minimal-pqc:latest
COPY --from=builder /app/cert-manager-approver-policy /usr/local/bin/cert-manager-approver-policy
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/cert-manager-approver-policy"]
