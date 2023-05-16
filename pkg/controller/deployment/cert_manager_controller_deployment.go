package deployment

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/staticresourcecontroller"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
)

const (
	certManagerControllerStaticResourcesControllerName = operatorName + "-controller-static-resources-"
	certManagerControllerDeploymentControllerName      = operatorName + "-controller-deployment"
	certManagerControllerDeploymentFile                = "cert-manager-deployment/controller/cert-manager-deployment.yaml"
)

var (
	certManagerControllerAssetFiles = []string{
		"cert-manager-deployment/cert-manager-namespace.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-approve-cert-manager-io-cr.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-approve-cert-manager-io-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-certificates-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-certificates-crb.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-certificatesigningrequests-cr.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-certificatesigningrequests-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-challenges-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-challenges-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-clusterissuers-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-clusterissuers-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-ingress-shim-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-ingress-shim-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-issuers-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-issuers-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-orders-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-controller-orders-crb.yaml",
		"cert-manager-deployment/controller/cert-manager-edit-cr.yaml",
		"cert-manager-deployment/controller/cert-manager-leaderelection-rb.yaml",
		"cert-manager-deployment/controller/cert-manager-leaderelection-role.yaml",
		"cert-manager-deployment/controller/cert-manager-sa.yaml",
		"cert-manager-deployment/controller/cert-manager-svc.yaml",
		"cert-manager-deployment/controller/cert-manager-view-cr.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-approve-cert-manager-io-cr.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-approve-cert-manager-io-crb.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-certificatesigningrequests-cr.yaml",
		"cert-manager-deployment/cert-manager/cert-manager-controller-certificatesigningrequests-crb.yaml",
	}
)

func NewCertManagerControllerStaticResourcesController(operatorClient v1helpers.OperatorClient,
	kubeClientContainer *resourceapply.ClientHolder,
	kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces,
	eventsRecorder events.Recorder) factory.Controller {
	return staticresourcecontroller.NewStaticResourceController(
		certManagerControllerStaticResourcesControllerName,
		assets.Asset,
		certManagerControllerAssetFiles,
		kubeClientContainer,
		operatorClient,
		eventsRecorder,
	).AddKubeInformers(kubeInformersForNamespaces)
}

func NewCertManagerControllerDeploymentController(operatorClient v1helpers.OperatorClientWithFinalizers,
	certManagerOperatorInformers certmanoperatorinformers.SharedInformerFactory,
	infraInformers configinformers.SharedInformerFactory,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	eventsRecorder events.Recorder, targetVersion string, versionRecorder status.VersionGetter, trustedCAConfigmapName, cloudCredentialsSecretName string) factory.Controller {
	return newGenericDeploymentController(
		certManagerControllerDeploymentControllerName,
		targetVersion,
		certManagerControllerDeploymentFile,
		operatorClient,
		certManagerOperatorInformers,
		infraInformers,
		kubeClient,
		kubeInformersForTargetNamespace,
		eventsRecorder,
		versionRecorder,
		trustedCAConfigmapName,
		cloudCredentialsSecretName,
	)
}
