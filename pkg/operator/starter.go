package operator

import (
	"context"
	"fmt"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"

	ctrl "sigs.k8s.io/controller-runtime"

	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	cm "github.com/openshift/cert-manager-operator/pkg/controller/certmanager"
	"github.com/openshift/cert-manager-operator/pkg/controller/deployment"
	"github.com/openshift/cert-manager-operator/pkg/controller/istiocsr"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

const (
	resyncInterval = 10 * time.Minute
)

// TrustedCAConfigMapName is the trusted ca configmap name
// provided as a runtime arg.
var TrustedCAConfigMapName string

// CloudSecretName is the name of the cloud secret to be
// used in ambient credentials mode, and is provided as a runtime arg
var CloudCredentialSecret string

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err
	}

	certManagerOperatorClient, err := certmanoperatorclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}

	apiExtensionsClient, err := apiextensionsclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}

	certManagerInformers := certmanoperatorinformers.NewSharedInformerFactory(certManagerOperatorClient, resyncInterval)

	operatorClient := &operatorclient.OperatorClient{
		Informers: certManagerInformers,
		Client:    certManagerOperatorClient.OperatorV1alpha1(),
	}

	// perform version changes to the version getter prior to tying it up in the status controller
	// via change-notification channel so that it only updates operator version in status once
	// either of the workloads synces
	versionRecorder := status.NewVersionGetter()
	versionRecorder.SetVersion("operator", status.VersionForOperatorFromEnv())

	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient,
		"",
		"kube-system",
		operatorclient.TargetNamespace,
	)

	configClient, err := configv1client.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err
	}
	infraInformers := configinformers.NewSharedInformerFactory(configClient, resyncInterval)

	certManagerControllerSet := deployment.NewCertManagerControllerSet(
		kubeClient,
		kubeInformersForNamespaces,
		kubeInformersForNamespaces.InformersFor(operatorclient.TargetNamespace),
		infraInformers,
		operatorClient,
		certManagerInformers,
		resourceapply.NewKubeClientHolder(kubeClient).WithAPIExtensionsClient(apiExtensionsClient),
		cc.EventRecorder,
		status.VersionForOperandFromEnv(),
		versionRecorder,
		TrustedCAConfigMapName,
		CloudCredentialSecret,
	)
	controllersToStart := certManagerControllerSet.ToArray()

	defaultCertManagerController := deployment.NewDefaultCertManagerController(
		operatorClient,
		certManagerOperatorClient.OperatorV1alpha1(),
		cc.EventRecorder,
	)

	controllersToStart = append(controllersToStart, defaultCertManagerController)

	for _, informer := range []interface{ Start(<-chan struct{}) }{
		certManagerInformers,
		kubeInformersForNamespaces,
		infraInformers,
	} {
		informer.Start(ctx.Done())
	}

	for _, controller := range controllersToStart {
		go controller.Run(ctx, 1)
	}

	manager, err := New()
	if err != nil {
		return fmt.Errorf("failed to create controller manager: %w", err)
	}

	cmReconciler := cm.NewCertManagerReconciler(manager.manager, runtimeFeatures)
	if err := cmReconciler.SetupWithManager(manager.manager); err != nil {
		return fmt.Errorf("failed to start cert-manager-ctrl-controller: %w", err)
	}

	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("failed to start ctrl controller manager: %w", err)
	}

	// TODO: fair to export manager rather than use manager.manager each time.
	istiocsrController, err := istiocsr.New(manager.manager)
	if err != nil {
		return fmt.Errorf("failed to initialize cert-manager-istio-csr-controller: %w", err)
	}

	// Feature-gated controller(s)
	featureControllers := FeatureControllerSetFactory(
		[]FeatureControllerSet{
			{
				FeatureName: v1alpha1.FeatureIstioCSR,
				controllers: []ManagedController{
					istiocsrController,
				},
				log: ctrl.Log.WithName("feature-controller-set"),
			},
		})

	go featureControllers.RunWithManagerOnceEnabled(ctrl.SetupSignalHandler(), manager.manager, runtimeFeatures)

	<-ctx.Done()
	return nil
}
