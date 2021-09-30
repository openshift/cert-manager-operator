# Cert Manager Operator for OpenShift

This repository contains Cert Manager Operator designed for OpenShift. The operator runs in `openshift-cert-manager-operator` namespace, whereas its operand in `cert-manager`. Both those namespaces are hardcoded.

## The operator architecture and design assumptions

The Operator uses the [upstream deployment manifests](https://github.com/jetstack/cert-manager/releases/download/v1.4.0/cert-manager.yaml). It divides them into separate files and deploys using 3 controllers:
- [cert_manager_cainjector_deployment.go](pkg/controller/deployment/cert_manager_cainjector_deployment.go)
- [cert_manager_controller_deployment.go](pkg/controller/deployment/cert_manager_controller_deployment.go)
- [cert_manager_webhook_deployment.go](pkg/controller/deployment/cert_manager_webhook_deployment.go)

The deployment is triggered upon creating a cluster-scoped `CertManager` object named `cluster`. An example might be found in the [deploy](deploy) directory.

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
