FROM registry.ci.openshift.org/ocp/builder:rhel-9-golang-1.22-openshift-4.17 AS builder

ARG RELEASE_BRANCH=release-1.15

ARG GO_BUILD_TAGS=strictfipsruntime,openssl
ENV GOEXPERIMENT strictfipsruntime 
ENV CGO_ENABLED 1

RUN mkdir -p /go/src/github.com/cert-manager
RUN git clone --depth 1 --branch $RELEASE_BRANCH https://github.com/openshift/jetstack-cert-manager.git /go/src/github.com/cert-manager/cert-manager
WORKDIR /go/src/github.com/cert-manager/cert-manager

RUN mkdir /app

# build acmesolver
WORKDIR /go/src/github.com/cert-manager/cert-manager/cmd/acmesolver
RUN go mod vendor
RUN go build -mod=vendor -tags $GO_BUILD_TAGS -o /app/_output/acmesolver main.go

# build cainjector
WORKDIR /go/src/github.com/cert-manager/cert-manager/cmd/cainjector
RUN go mod vendor
RUN go build -mod=vendor -tags $GO_BUILD_TAGS -o /app/_output/cainjector main.go

# build controller
WORKDIR /go/src/github.com/cert-manager/cert-manager/cmd/controller
RUN go mod vendor
RUN go build -mod=vendor -tags $GO_BUILD_TAGS -o /app/_output/controller main.go

# build webhook
WORKDIR /go/src/github.com/cert-manager/cert-manager/cmd/webhook
RUN go mod vendor
RUN go build -mod=vendor -tags $GO_BUILD_TAGS -o /app/_output/webhook main.go


FROM registry.access.redhat.com/ubi9-minimal:9.4-1227.1726694542

COPY --from=builder /app/_output/acmesolver /app/cmd/acmesolver/acmesolver
COPY --from=builder /app/_output/cainjector /app/cmd/cainjector/cainjector
COPY --from=builder /app/_output/controller /app/cmd/controller/controller
COPY --from=builder /app/_output/webhook /app/cmd/webhook/webhook

USER 65532:65532
