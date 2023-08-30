# Cert Manager Operator for OpenShift

This repository contains Cert Manager Operator designed for OpenShift. The operator runs in `cert-manager-operator` namespace, whereas its operand in `cert-manager`. Both those namespaces are hardcoded.

## The operator architecture and design assumptions

The Operator uses the [upstream deployment manifests](https://github.com/jetstack/cert-manager/releases/download/v1.12.3/cert-manager.yaml). It divides them into separate files and deploys using 3 controllers:
- [cert_manager_cainjector_deployment.go](pkg/controller/deployment/cert_manager_cainjector_deployment.go)
- [cert_manager_controller_deployment.go](pkg/controller/deployment/cert_manager_controller_deployment.go)
- [cert_manager_webhook_deployment.go](pkg/controller/deployment/cert_manager_webhook_deployment.go)

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
+- cmd
+- deploy
  +- examples - Examples to make testing easier
+- hack - All sorts of scripts
+- images
  +- ci - Dockerfile
+- config - Template for generating OLM bundle
+- pkg
+- tools
+- vendor
```

## Running the operator locally (development)

Connect to your OpenShift cluster and run the following command:

    make local-run

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

2. _Optional_: you may need to link the registry secret to `cert-manager-operator` service account if the image is not public ([Doc link](https://docs.openshift.com/container-platform/4.10/openshift_images/managing_images/using-image-pull-secrets.html#images-allow-pods-to-reference-images-from-secure-registries_using-image-pull-secrets)):

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

## Updating resources

Use the following command to update all generated resources:

    make update

## Upgrading cert-manager

Update the version of cert-manager in the `Makefile`:

```shell
  $ git diff Makefile
  diff --git a/Makefile b/Makefile
  index e414cc7..d04d45d 100644
  --- a/Makefile
  +++ b/Makefile
  @@ -13,7 +13,7 @@ BUNDLE_IMAGE_TAG?=latest
  
  TEST_OPERATOR_NAMESPACE?=cert-manager-operator
  
  -MANIFEST_SOURCE = https://github.com/jetstack/cert-manager/releases/download/v1.5.4/cert-manager.yaml
  +MANIFEST_SOURCE = https://github.com/jetstack/cert-manager/releases/download/v1.6.1/cert-manager.yaml
  
  OPERATOR_SDK_VERSION?=v1.12.0
  OPERATOR_SDK?=$(PERMANENT_TMP_GOPATH)/bin/operator-sdk-$(OPERATOR_SDK_VERSION)
```

Execute the `update-manifests` target:

```shell
$ make update
hack/update-cert-manager-manifests.sh https://github.com/jetstack/cert-manager/releases/download/v1.6.1/cert-manager.yaml
---- Downloading manifest file from https://github.com/jetstack/cert-manager/releases/download/v1.6.1/cert-manager.yaml ----
---- Installing tooling ----
---- Patching manifest ----
cert-manager-crds/certificaterequests.cert-manager.io-crd.yaml
...
```

Check the changes in the `bindata/` folder and assert any inconsistencies or errors.

## Running e2e tests locally

The testsuite assumes, that Cert Manager Operator has been successfully deployed 
in the cluster and it also successfully deployed Cert Manager (the operand). This
is exactly what Prow is doing in cooperation with 

`make test-e2e-wait-for-stable-state`.

If you'd like to run all the tests locally, you need to ensure the same requirements
are met. The easiest way to do it follow steps from above.

Then, let it run for a few minutes. Once the operands are deployed, just invoke:

    make test-e2e

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

The guide to [enable the cert-manager metrics and monitoring](https://github.com/openshift/cert-manager-operator/tree/master/docs/OPERAND_METRICS.md) will help you get started.
