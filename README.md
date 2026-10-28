# Cert Manager Operator for OpenShift

This repository contains Cert Manager Operator designed for OpenShift. The operator runs in `cert-manager-operator` namespace, whereas its operand in `cert-manager`. Both those namespaces are hardcoded.

## The operator architecture and design assumptions

The Operator uses the [upstream deployment manifests](https://github.com/cert-manager/cert-manager/releases). It divides them into separate files and deploys using 3 controllers:
- [cert_manager_cainjector_deployment.go](pkg/controller/certmanager/cert_manager_cainjector_deployment.go)
- [cert_manager_controller_deployment.go](pkg/controller/certmanager/cert_manager_controller_deployment.go)
- [cert_manager_webhook_deployment.go](pkg/controller/certmanager/cert_manager_webhook_deployment.go)

The Operator automatically deploys a cluster-scoped `CertManager` object named `cluster` if it's missing (with default values).

### Directory structure

```
+- api - The API type
+- bindata
  +- cert-manager-crds - CRDs for Cert Manager
  +- cert-manager-deployment - Deployment Manifests for Cert Manager
+- bundle
  +- cert-manager-operator
    +- manifests - This operator's CRDs
+- config - Template for generating OLM bundle
+- deploy
  +- examples - Examples to make testing easier
+- docs - Product documentation (proxy, cloud credentials, metrics)
+- hack - All sorts of scripts
+- harness-evals - Architecture docs, ADRs, and coding guidelines
+- images
  +- ci - Dockerfile
+- pkg
+- test - End-to-end and integration tests
+- tools
+- vendor
```

## Running the operator locally (development)

Connect to your OpenShift cluster and run the following command:

```sh
make deploy
oc scale --replicas=0 deploy --all -n cert-manager-operator
make local-run
```

This command will install all the necessary Operator manifests as well as all necessary CRDs. After this part is complete, it will run the Operator locally.

## Running the operator in the cluster

### Preparing the environment
Prepare your environment for the installation commands.

- Select the container runtime you want to build the images with (`podman` or `docker`):
    ```sh
    export CONTAINER_ENGINE=podman
    ```
- Select the name settings of the image:
    ```sh
    export REGISTRY=quay.io
    export REPOSITORY=myuser
    export IMAGE_VERSION=1.0.0
    ```
- Login to the image registry:
    ```sh
    ${CONTAINER_ENGINE} login ${REGISTRY} -u ${REPOSITORY}
    ```

### Installing the Cert Manager Operator by building and pushing the Operator image to a registry
1. Build and push the Operator image to a registry:
   ```sh
   export IMG=${REGISTRY}/${REPOSITORY}/cert-manager-operator:${IMAGE_VERSION}
   make image-build image-push
   ```

   Note: If you're on a non-x86 arch like arm64, you may need to use commands like `docker buildx build --platform linux/amd64` or `podman build --platform linux/amd64` to specific the target platforms in the Makefile. (Docs link: [Docker](https://docs.docker.com/engine/reference/commandline/buildx_build/#platform), [Podman](https://docs.podman.io/en/stable/markdown/podman-build.1.html#platform-os-arch-variant))

2. _Optional_: you may need to link the registry secret to `cert-manager-operator` service account if the image is not public ([Doc link](https://docs.openshift.com/container-platform/latest/openshift_images/managing_images/using-image-pull-secrets.html#images-allow-pods-to-reference-images-from-secure-registries_using-image-pull-secrets)):

    a. Create a secret with authentication details of your image registry:
    ```sh
    oc -n cert-manager-operator create secret generic certmanager-pull-secret  --type=kubernetes.io/dockercfg  --from-file=.dockercfg=${XDG_RUNTIME_DIR}/containers/auth.json
    ```
    b. Link the secret to `cert-manager-operator` service account:
    ```sh
    oc -n cert-manager-operator secrets link cert-manager-operator certmanager-pull-secret --for=pull
    ````

3. Run the following command to deploy the Cert Manager Operator:
    ```sh
    make deploy
    ```

### Cleaning up the deployment

To remove the Cert Manager Operator and its associated resources from the cluster, run the following command:

```sh
make undeploy
```

This will delete all resources created during the deployment process, including the operator and operand resources.

## Updating resources

Use the following command to update all generated resources:

    make update

## Upgrading cert-manager

Update `CERT_MANAGER_VERSION` in the `Makefile`, then regenerate the bundled manifests:

```shell
make update-manifests
```

After regeneration, update the matching `RELATED_IMAGE_*` env vars and `*_OPERAND_IMAGE_VERSION` constants in `config/manager/manager.yaml` to match, then run:

```shell
make bundle
```

Check the changes in the `bindata/` folder and assert any inconsistencies or errors. See [harness-evals/harness-docs/CERT_MANAGER_OPERATOR_DEVELOPMENT.md](harness-evals/harness-docs/CERT_MANAGER_OPERATOR_DEVELOPMENT.md) for the full bump checklist.

## Running tests locally

To run all unit tests locally, use the following command:

    make test

This will execute all unit tests and generate a coverage report (`cover.out`).

## Running e2e tests locally

The testsuite assumes, that Cert Manager Operator has been successfully deployed 
in the cluster and it also successfully deployed Cert Manager (the operand). This
is exactly what Prow is doing in cooperation with 

`make test-e2e-wait-for-stable-state`.

If you'd like to run all the tests locally, you need to ensure the same requirements
are met. The easiest way to do it follow steps from above.

Then, let it run for a few minutes. Once the operands are deployed, just invoke:

    make test-e2e

## Linting the code

To ensure the code adheres to the project's linting rules, run:

    make lint

This will use `golangci-lint` to check for any linting issues in the codebase.

## Using unsupported config overrides options

It is possible (although not supported) to specify custom settings to each Cert Manager image. In order to do it,
you need to modify the `certmanager.operator/cluster` object:

```asciidoc
apiVersion: operator.openshift.io/v1alpha1
kind: CertManager
metadata:
  name: cluster
spec:
  managementState: "Managed"
  unsupportedConfigOverrides:
    # Here's an example to supply custom DNS settings.
    controller:
      args:
        - "--dns01-recursive-nameservers=1.1.1.1:53"
        - "--dns01-recursive-nameservers-only"
```
## Metrics and Monitoring

The guide to [enable the cert-manager metrics and monitoring](docs/operand_metrics.md) will help you get started.
