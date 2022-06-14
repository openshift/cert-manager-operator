# Cert Manager Operator for OpenShift

This repository contains Cert Manager Operator designed for OpenShift. The operator runs in `openshift-cert-manager-operator` namespace, whereas its operand in `cert-manager`. Both those namespaces are hardcoded.

## The operator architecture and design assumptions

The Operator uses the [upstream deployment manifests](https://github.com/jetstack/cert-manager/releases/download/v1.4.0/cert-manager.yaml). It divides them into separate files and deploys using 3 controllers:
- [cert_manager_cainjector_deployment.go](pkg/controller/deployment/cert_manager_cainjector_deployment.go)
- [cert_manager_controller_deployment.go](pkg/controller/deployment/cert_manager_controller_deployment.go)
- [cert_manager_webhook_deployment.go](pkg/controller/deployment/cert_manager_webhook_deployment.go)

The Operator automatically deploys a cluster-scoped `CertManager` object named `cluster` if it's missing (with default values).

### Directory structure

```
+- apis - The API types
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
+- manifests - Manifests required for deploying this operator
+- pkg
+- vendor
```

## Running the operator locally (development)

Connect to your OpenShift cluster and run the following command:

    make local-run

This command will install all the necessary Operator manifests as well as all necessary CRDs. After this part is complete, it will run the Operator locally.

## Running the operator in the cluster

Connect to your OpenShift cluster and run the following command:

    make operator-clean operator-push-bundle operator-run-bundle IMAGE_REGISTRY=<registry>/<org>

This command will:
- remove any existing operator that might be in your cluster
- build and push the bundle into `<registry>/<org>/cert-manager-operator-bundle:latest`
- download Operator SDK if necessary
- install the bundle into your cluster

!!! WARNING !!!

As for the time-being, the bundle uses a hardcoded image in `slaskawi`'s repo. This will change when we start building
the Operator images on each commit.

!!! WARNING !!!

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
  
  TEST_OPERATOR_NAMESPACE?=openshift-cert-manager-operator
  
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

The testsuite assumes, that Cert Manager Operator has been successfully deployed in the cluster and 
it also successfully deployed Cert Manager (the operand). This is exactly what Prow is doing in cooperation with 
`make test-e2e-wait-for-stable-state`.

If you'd like to run all the tests locally, you need to ensure the same requirements are met. The easiest way to do it
is to:

    make local-deploy-manifests local-run

Then, let it run for a few minutes. Once the operands are deployed, just invoke:

    make test-e2e -o test-e2e-wait-for-stable-state

The `-o test-e2e-wait-for-stable-state` skips checking if the operator is deployed. Depending on how do you're testing it,
you may or may not want to invoke it.

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
        - --v=2
        - --cluster-resource-namespace=$(POD_NAMESPACE)
        - --leader-election-namespace=kube-system
        - --dns01-recursive-nameservers-only
        - --dns01-recursive-nameservers=8.8.8.8:53,8.8.4.4:53
```
