package operator

import (
	"context"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/cert-manager-operator/pkg/operator/kubeclient"

	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/controller/deployment"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

const (
	resyncInterval = 10 * time.Minute
)

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
	kubeConfigContainer, err := kubeclient.NewKubeClientContainer(cc.ProtoKubeConfig, cc.KubeConfig)
	if err != nil {
		return err
	}

	certManagerInformers := certmanoperatorinformers.NewSharedInformerFactory(kubeConfigContainer.CertManagerOperatorClient, resyncInterval)

	operatorClient := &operatorclient.OperatorClient{
		Informers: certManagerInformers,
		Client:    kubeConfigContainer.CertManagerOperatorClient.OperatorV1alpha1(),
	}

	clusterOperator, err := kubeConfigContainer.ConfigClient.ConfigV1().ClusterOperators().Get(ctx, "cert-manager-operator", metav1.GetOptions{})
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

	kubeInformersForTargetNamespace := v1helpers.NewKubeInformersForNamespaces(kubeConfigContainer.KubeConfig,
		"",
		"kube-system",
		"cert-manager",
		operatorclient.TargetNamespace,
	)

	configInformers := configinformers.NewSharedInformerFactory(kubeConfigContainer.ConfigClient, resyncInterval)

	certManagerControllerSet := deployment.NewCertManagerControllerSet(kubeInformersForTargetNamespace, kubeInformersForTargetNamespace.InformersFor(operatorclient.TargetNamespace), operatorClient, kubeConfigContainer, cc.EventRecorder, versionRecorder)
	controllersToStart := certManagerControllerSet.ToArray()

	statusController := status.NewClusterOperatorStatusController(
		"cert-manager",
		[]configv1.ObjectReference{
			{Resource: "namespaces", Name: operatorclient.TargetNamespace},
		},
		kubeConfigContainer.ConfigClient.ConfigV1(),
		configInformers.Config().V1().ClusterOperators(),
		operatorClient,
		versionRecorder,
		cc.EventRecorder,
	)

	controllersToStart = append(controllersToStart, statusController)

	for _, informer := range []interface{ Start(<-chan struct{}) }{
		configInformers,
		certManagerInformers,
		kubeInformersForTargetNamespace,
	} {
		informer.Start(ctx.Done())
	}

	for _, controller := range controllersToStart {
		go controller.Run(ctx, 1)
	}

	<-ctx.Done()
	return nil
}
