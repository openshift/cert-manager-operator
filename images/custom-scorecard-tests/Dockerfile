FROM registry.ci.openshift.org/ocp/builder:rhel-8-golang-1.17-openshift-4.10 AS builder
ARG TARGETARCH

WORKDIR /workspace

# Copy the go source
COPY . .

# Build
RUN GOARCH=$TARGETARCH BUILD_DIR=/workspace make build/custom-scorecard-tests

FROM registry.ci.openshift.org/ocp/4.10:base

COPY --from=builder /workspace/custom-scorecard-tests /usr/local/bin/custom-scorecard-tests

ENTRYPOINT ["/usr/local/bin/custom-scorecard-tests"]