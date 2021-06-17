package deployment

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/apiserver/controller/workload"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
)

type certManagerDeploymentController struct{}

func NewCertManagerDeploymentController(
	operatorNamespace, targetNamespace, targetVersion string,
	operatorClient v1helpers.OperatorClient,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	openshiftClusterConfigClient configv1.ClusterOperatorInterface,
	eventsRecorder events.Recorder,
	versionRecorder status.VersionGetter,
) factory.Controller {
	controller := &certManagerDeploymentController{}

	return workload.NewController(
		"cert-manager",
		operatorNamespace,
		targetNamespace,
		targetVersion,
		"",
		"CertManager",
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace.Core().V1().Pods().Lister(),
		nil,
		[]factory.Informer{
			kubeInformersForTargetNamespace.Apps().V1().Deployments().Informer(),
			kubeInformersForTargetNamespace.Core().V1().Pods().Informer(),
			kubeInformersForTargetNamespace.Core().V1().Namespaces().Informer(),
		},
		controller,
		openshiftClusterConfigClient,
		eventsRecorder,
		versionRecorder,
	)
}

func (c *certManagerDeploymentController) Sync(ctx context.Context, syncContext factory.SyncContext) (*appsv1.Deployment, bool, []error) {
	return nil, true, nil
}

func (c *certManagerDeploymentController) PreconditionFulfilled(ctx context.Context) (bool, error) {
	return true, nil
}
