package operator

import (
	"context"
	"fmt"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/client-go/kubernetes"
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
	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
	if err != nil {
		return err //nolint:wrapcheck // error from NewForConfig is already contextual
	}

	certManagerOperatorClient, err := certmanoperatorclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err //nolint:wrapcheck // error from NewForConfig is already contextual
	}

	apiExtensionsClient, err := apiextensionsclient.NewForConfig(cc.KubeConfig)
	if err != nil {
		return err //nolint:wrapcheck // error from NewForConfig is already contextual
	}

	certManagerInformers := certmanoperatorinformers.NewSharedInformerFactory(certManagerOperatorClient, resyncInterval)

	operatorClient := &operatorclient.OperatorClient{
		Informers: certManagerInformers,
		Client:    certManagerOperatorClient.OperatorV1alpha1(),
		Clock:     cc.Clock,
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
		return err //nolint:wrapcheck // error from NewForConfig is already contextual
	}

	infraGVR := configv1.GroupVersion.WithResource("infrastructures")
	optInfraInformer, err := optionalinformer.NewOptionalInformer(
		ctx, infraGVR, configClient.Discovery(),
		func() configinformers.SharedInformerFactory {
			return configinformers.NewSharedInformerFactory(configClient, resyncInterval)
		},
	)
	if err != nil {
		return fmt.Errorf("failed to discover Infrastructure presence: %w", err)
	}

	certManagerControllerSet := deployment.NewCertManagerControllerSet(
		kubeClient,
		kubeInformersForNamespaces,
		kubeInformersForNamespaces.InformersFor(operatorclient.TargetNamespace),
		*optInfraInformer,
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
	} {
		informer.Start(ctx.Done())
	}

	// only start the informer if Infrastructure is found applicable
	if optInfraInformer.Applicable() {
		(*optInfraInformer.InformerFactory).Start(ctx.Done())
	}

	for _, controller := range controllersToStart {
		go controller.Run(ctx, 1)
	}

	err = features.SetupWithFlagValue(UnsupportedAddonFeatures)
	if err != nil {
		return fmt.Errorf("failed to parse addon features: %w", err)
	}

	// enable controller-runtime and istio-csr controller
	// only when "IstioCSR" feature is turned on from --addon-features
	if features.DefaultFeatureGate.Enabled(v1alpha1.FeatureIstioCSR) {
		manager, err := NewControllerManager()
		if err != nil {
			return fmt.Errorf("failed to create controller manager: %w", err)
		}
		if err := manager.Start(ctrl.SetupSignalHandler()); err != nil { //nolint:contextcheck // SetupSignalHandler creates a new context for signal handling, which is intentional
			return fmt.Errorf("failed to start istiocsr controller: %w", err)
		}
	}

	<-ctx.Done()
	return nil
}
