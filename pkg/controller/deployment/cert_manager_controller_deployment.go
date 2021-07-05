package deployment

import (
	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"k8s.io/client-go/informers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/kubeclient"
)

const (
	certManagerControllerStaticResourcesControllerName = operatorName + "-controller-static-resources-"
	certManagerControllerDeploymentControllerName      = operatorName + "-controller-deployment-"
	certManagerControllerDeploymentFile                = "cert-manager-deployment/cert-manager-controller/cert-manager-deployment.yaml"
)

var (
	certManagerControllerAssetFiles = []string{
		"cert-manager-crds/certificaterequests.cert-manager.io-crd.yaml",
		"cert-manager-crds/certificates.cert-manager.io-crd.yaml",
		"cert-manager-crds/challenges.acme.cert-manager.io-crd.yaml",
		"cert-manager-crds/clusterissuers.cert-manager.io-crd.yaml",
		"cert-manager-crds/issuers.cert-manager.io-crd.yaml",
		"cert-manager-crds/orders.acme.cert-manager.io-crd.yaml",
		"cert-manager-deployment/cert-manager-namespace.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-approve-cert-manager-io-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-approve-cert-manager-io-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-certificates-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-certificates-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-certificatesigningrequests-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-certificatesigningrequests-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-challenges-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-challenges-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-clusterissuers-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-clusterissuers-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-ingress-shim-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-ingress-shim-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-issuers-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-issuers-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-orders-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-controller-orders-crb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-edit-cr.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-leaderelection-rb.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-leaderelection-role.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-sa.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-svc.yaml",
		"cert-manager-deployment/cert-manager-controller/cert-manager-view-cr.yaml",
	}
)

func NewCertManagerControllerStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer kubeclient.KubeClientContainer,
	kubeInformersForTargetNamespace v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerControllerStaticResourcesControllerName,
		assets.Asset,
		certManagerControllerAssetFiles,
		kubeClientContainer.ToKubeClientHolder(),
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForTargetNamespace)
}

func NewCertManagerControllerDeploymentController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer kubeclient.KubeClientContainer,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	openshiftClusterConfigClient configv1.ClusterOperatorInterface,
	eventsRecorder events.Recorder, versionRecorder status.VersionGetter) factory.Controller {
	return newGenericDeploymentController(
		certManagerControllerDeploymentControllerName,
		certManagerControllerDeploymentFile,
		operatorClient,
		kubeClientContainer,
		kubeInformersForTargetNamespace,
		openshiftClusterConfigClient,
		eventsRecorder,
		versionRecorder)
}
