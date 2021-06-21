package operator

import (
	"context"
	"os"
	"time"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/genericoperatorclient"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/controller/deployment"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	configClient, err := configv1client.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}

	genericOperatorClient, dynamicInformers, err := genericoperatorclient.NewClusterScopedOperatorClient(
		cc.KubeConfig, operatorv1.GroupVersion.WithResource("certmanagers"))
	if err != nil {
		return err
	}

	clusterOperator, err := configClient.ConfigV1().ClusterOperators().Get(ctx, "cert-manager-operator", metav1.GetOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	// perform version changes to the version getter prior to tying it up in the status controller
	// via change-notification channel so that it only updates operator version in status once
	// either of the workloads synces
	versionRecorder := status.NewVersionGetter()
	for _, version := range clusterOperator.Status.Versions {
		versionRecorder.SetVersion(version.Name, version.Version)
	}
	versionRecorder.SetVersion("operator", status.VersionForOperatorFromEnv())

	kubeInformersForTargetNamespace := v1helpers.NewKubeInformersForNamespaces(kubeClient,
		operatorclient.TargetNamespace,
	)

	configInformers := configinformers.NewSharedInformerFactory(configClient, 10*time.Minute)

	statusController := status.NewClusterOperatorStatusController(
		"cert-manager",
		[]configv1.ObjectReference{
			{Resource: "namespaces", Name: operatorclient.TargetNamespace},
		},
		configClient.ConfigV1(),
		configInformers.Config().V1().ClusterOperators(),
		genericOperatorClient,
		versionRecorder,
		cc.EventRecorder,
	)

	deploymentController := deployment.NewCertManagerDeploymentController(
		operatorclient.TargetNamespace, operatorclient.TargetNamespace,
		os.Getenv("OPERAND_CERT_MANAGER_IMAGE_VERSION"),
		genericOperatorClient,
		kubeClient,
		kubeInformersForTargetNamespace.InformersFor(operatorclient.TargetNamespace),
		configClient.ConfigV1().ClusterOperators(),
		cc.EventRecorder,
		versionRecorder,
	)

	for _, informer := range []interface{ Start(<-chan struct{}) }{
		configInformers,
		dynamicInformers,
		kubeInformersForTargetNamespace,
	} {
		informer.Start(ctx.Done())
	}

	for _, controller := range []interface{ Run(context.Context, int) }{
		statusController,
		deploymentController,
	} {
		go controller.Run(ctx, 1)
	}

	<-ctx.Done()
	return nil
}
