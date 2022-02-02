package deployment

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/apiserver/controller/workload"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"github.com/openshift/library-go/pkg/operator/resource/resourceread"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/operator/assets"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

type genericDeploymentController struct {
	kubeClient     kubernetes.Interface
	operatorClient v1helpers.OperatorClient

	deploymentFile string
}

func newGenericDeploymentController(
	controllerName, targetVersion, deploymentFile string,
	operatorClient v1helpers.OperatorClient,
	kubeClient kubernetes.Interface,
	kubeInformersForTargetNamespace informers.SharedInformerFactory,
	openshiftClusterConfigClient configv1.ClusterOperatorInterface,
	eventsRecorder events.Recorder,
	versionRecorder status.VersionGetter,
) factory.Controller {
	controller := &genericDeploymentController{
		kubeClient:     kubeClient,
		operatorClient: operatorClient,

		deploymentFile: deploymentFile,
	}

	return workload.NewController(
		controllerName,
		operatorclient.TargetNamespace,
		operatorclient.TargetNamespace,
		targetVersion,
		operandNamePrefix,
		conditionsPrefix,
		operatorClient,
		kubeClient,
		kubeInformersForTargetNamespace.Core().V1().Pods().Lister(),
		[]factory.Informer{
			operatorClient.Informer(),
		},
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

func (c *genericDeploymentController) Sync(ctx context.Context, syncContext factory.SyncContext) (*appsv1.Deployment, bool, []error) {
	var errors []error
	var err error
	var appliedDeployment *appsv1.Deployment

	assert, _ := assets.Asset(c.deploymentFile)
	deployment := resourceread.ReadDeploymentV1OrDie(assert)
	for index := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[index].Image = certManagerImage(deployment.Spec.Template.Spec.Containers[index].Image)
	}
	_, opStatus, _, _ := c.operatorClient.GetOperatorState()
	appliedDeployment, _, err = resourceapply.ApplyDeployment(ctx, c.kubeClient.AppsV1(), syncContext.Recorder(), deployment, resourcemerge.ExpectedDeploymentGeneration(deployment, opStatus.Generations))

	if err != nil {
		return nil, false, append(errors, fmt.Errorf("applying deployment %v failed: %w", deployment.Name, err))
	}

	return appliedDeployment, len(errors) == 0, errors
}

func (c *genericDeploymentController) PreconditionFulfilled(ctx context.Context) (bool, error) {
	return true, nil
}
