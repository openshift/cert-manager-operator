package operator

import (
	"context"
	"fmt"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"

	ctrl "sigs.k8s.io/controller-runtime"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/deployment"
	"github.com/openshift/cert-manager-operator/pkg/features"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	"github.com/openshift/cert-manager-operator/pkg/operator/optionalinformer"
)

const (
	resyncInterval = 10 * time.Minute
)

// TrustedCAConfigMapName is the trusted ca configmap name
// provided as a runtime arg.
var TrustedCAConfigMapName string

// CloudSecretName is the name of the cloud secret to be
// used in ambient credentials mode, and is provided as a runtime arg.
var CloudCredentialSecret string

// UnsupportedAddonFeatures is the user-specific list of unsupported addon features
// that the operator optionally enables, and is provided as a runtime arg.
var UnsupportedAddonFeatures string

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
	clients, err := initializeClients(cc)
	if err != nil {
		return err
	}

	operatorClient, versionRecorder := initializeOperatorComponents(clients.certManagerInformers, clients.certManagerOperatorClient, cc.Clock)

	kubeInformersForNamespaces := initializeKubeInformers(clients.kubeClient)
	optInfraInformer, err := initializeInfraInformer(ctx, clients.configClient)
	if err != nil {
		return err
	}

	controllersToStart := buildControllers(clients, operatorClient, versionRecorder, kubeInformersForNamespaces, optInfraInformer, cc)
	startInformers(ctx, clients.certManagerInformers, kubeInformersForNamespaces, optInfraInformer)
	startControllers(ctx, controllersToStart)

	if err := setupFeatures(); err != nil {
		return err
	}

	if err := startIstioCSRController(); err != nil {
		return err
	}

	<-ctx.Done()
	return nil
}

type operatorClients struct {
	kubeClient                kubernetes.Interface
	certManagerOperatorClient certmanoperatorclient.Interface
	apiExtensionsClient       apiextensionsclient.Interface
	configClient              configv1client.Interface
	certManagerInformers      certmanoperatorinformers.SharedInformerFactory
}

func initializeClients(cc *controllercmd.ControllerContext) (*operatorClients, error) {
	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return nil, err
	}

	certManagerOperatorClient, err := certmanoperatorclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return nil, err
	}

	apiExtensionsClient, err := apiextensionsclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return nil, err
	}

	configClient, err := configv1client.NewForConfig(cc.KubeConfig)
	if err != nil {
		return nil, err
	}

	certManagerInformers := certmanoperatorinformers.NewSharedInformerFactory(certManagerOperatorClient, resyncInterval)

	return &operatorClients{
		kubeClient:                kubeClient,
		certManagerOperatorClient: certManagerOperatorClient,
		apiExtensionsClient:       apiExtensionsClient,
		configClient:              configClient,
		certManagerInformers:      certManagerInformers,
	}, nil
}

func initializeOperatorComponents(certManagerInformers certmanoperatorinformers.SharedInformerFactory, certManagerOperatorClient certmanoperatorclient.Interface, clock clock.PassiveClock) (*operatorclient.OperatorClient, status.VersionGetter) {
	operatorClient := &operatorclient.OperatorClient{
		Informers: certManagerInformers,
		Client:    certManagerOperatorClient.OperatorV1alpha1(),
		Clock:     clock,
	}

	versionRecorder := status.NewVersionGetter()
	versionRecorder.SetVersion("operator", status.VersionForOperatorFromEnv())

	return operatorClient, versionRecorder
}

func initializeKubeInformers(kubeClient kubernetes.Interface) v1helpers.KubeInformersForNamespaces {
	return v1helpers.NewKubeInformersForNamespaces(kubeClient,
		"",
		"kube-system",
		operatorclient.TargetNamespace,
	)
}

func initializeInfraInformer(ctx context.Context, configClient configv1client.Interface) (*optionalinformer.OptionalInformer[configinformers.SharedInformerFactory], error) {
	infraGVR := configv1.GroupVersion.WithResource("infrastructures")
	optInfraInformer, err := optionalinformer.NewOptionalInformer(
		ctx, infraGVR, configClient.Discovery(),
		func() configinformers.SharedInformerFactory {
			return configinformers.NewSharedInformerFactory(configClient, resyncInterval)
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to discover Infrastructure presence: %w", err)
	}
	return optInfraInformer, nil
}

func buildControllers(clients *operatorClients, operatorClient *operatorclient.OperatorClient, versionRecorder status.VersionGetter, kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces, optInfraInformer *optionalinformer.OptionalInformer[configinformers.SharedInformerFactory], cc *controllercmd.ControllerContext) []interface{ Run(context.Context, int) } {
	certManagerControllerSet := deployment.NewCertManagerControllerSet(
		clients.kubeClient,
		kubeInformersForNamespaces,
		kubeInformersForNamespaces.InformersFor(operatorclient.TargetNamespace),
		*optInfraInformer,
		operatorClient,
		clients.certManagerInformers,
		resourceapply.NewKubeClientHolder(clients.kubeClient).WithAPIExtensionsClient(clients.apiExtensionsClient),
		cc.EventRecorder,
		status.VersionForOperandFromEnv(),
		versionRecorder,
		TrustedCAConfigMapName,
		CloudCredentialSecret,
	)
	controllersToStart := certManagerControllerSet.ToArray()

	defaultCertManagerController := deployment.NewDefaultCertManagerController(
		operatorClient,
		clients.certManagerOperatorClient.OperatorV1alpha1(),
		cc.EventRecorder,
	)

	allControllers := make([]interface{ Run(context.Context, int) }, 0, len(controllersToStart)+1)
	for _, c := range controllersToStart {
		allControllers = append(allControllers, c)
	}
	allControllers = append(allControllers, defaultCertManagerController)
	return allControllers
}

func startInformers(ctx context.Context, certManagerInformers certmanoperatorinformers.SharedInformerFactory, kubeInformersForNamespaces v1helpers.KubeInformersForNamespaces, optInfraInformer *optionalinformer.OptionalInformer[configinformers.SharedInformerFactory]) {
	for _, informer := range []interface{ Start(<-chan struct{}) }{
		certManagerInformers,
		kubeInformersForNamespaces,
	} {
		informer.Start(ctx.Done())
	}

	if optInfraInformer.Applicable() {
		(*optInfraInformer.InformerFactory).Start(ctx.Done())
	}
}

func startControllers(ctx context.Context, controllersToStart []interface{ Run(context.Context, int) }) {
	for _, controller := range controllersToStart {
		go controller.Run(ctx, 1)
	}
}

func setupFeatures() error {
	if err := features.SetupWithFlagValue(UnsupportedAddonFeatures); err != nil {
		return fmt.Errorf("failed to parse addon features: %w", err)
	}
	return nil
}

func startIstioCSRController() error {
	if !features.DefaultFeatureGate.Enabled(v1alpha1.FeatureIstioCSR) {
		return nil
	}

	manager, err := NewControllerManager()
	if err != nil {
		return fmt.Errorf("failed to create controller manager: %w", err)
	}
	if err := manager.Start(ctrl.SetupSignalHandler()); err != nil {
		return fmt.Errorf("failed to start istiocsr controller: %w", err)
	}
	return nil
}
