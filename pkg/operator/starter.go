package operator

import (
	"context"
	"fmt"
	"os"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiserver "k8s.io/apiserver/pkg/server"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned"
	configinformers "github.com/openshift/client-go/config/informers/externalversions"
	consoleclient "github.com/openshift/client-go/console/clientset/versioned"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"

	"github.com/openshift/cert-manager-operator/pkg/controller/certmanager"
	"github.com/openshift/cert-manager-operator/pkg/features"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
	"github.com/openshift/cert-manager-operator/pkg/operator/utils"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
)

const (
	resyncInterval = 10 * time.Minute
)

// TrustedCAConfigMapName is the trusted ca configmap name
// provided as a runtime arg.
var TrustedCAConfigMapName string

// CloudCredentialSecret is the name of the cloud secret to be
// used in ambient credentials mode, and is provided as a runtime arg.
var CloudCredentialSecret string

// UnsupportedAddonFeatures is the user-specific list of unsupported addon features
// that the operator optionally enables, and is provided as a runtime arg.
var UnsupportedAddonFeatures string

// OperatorTLSWatch is the cluster TLS profile applied (or deliberately skipped)
// on the operator metrics server at process start. Enabled is false when there
// is no HTTPS server to reconfigure, or when startup could not read a baseline.
type OperatorTLSWatch struct {
	Resolved tlsprofile.ResolvedProfile
	Enabled  bool
}

func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext, tlsWatch OperatorTLSWatch) error {
	// Set controller-runtime logger before any ctrl.Log usage (e.g. NewControllerManager).
	// Uses klog so --v flag controls verbosity for both library-go and controller-runtime.
	ctrl.SetLogger(klog.NewKlogr())

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
		Clock:     cc.Clock,
	}

	// perform version changes to the version getter prior to tying it up in the status controller
	// via change-notification channel so that it only updates operator version in status once
	// either of the workloads syncs.
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

	if err := startTLSSecurityProfileWatcher(ctx, configClient, tlsWatch); err != nil {
		return err
	}

	infraGVR := configv1.GroupVersion.WithResource("infrastructures")
	optInfraInformer, err := utils.InitInformerIfAvailable(
		utils.NewResourceDiscoverer(infraGVR, configClient.Discovery()),
		func() configinformers.SharedInformerFactory {
			return configinformers.NewSharedInformerFactory(configClient, resyncInterval)
		},
	)
	if err != nil {
		return fmt.Errorf("failed to discover Infrastructure presence: %w", err)
	}

	consoleDiscoverer := utils.NewResourceDiscoverer(certmanager.ConsoleYAMLSampleGVR, configClient.Discovery())
	consoleAvailable, err := consoleDiscoverer.Discover()
	if err != nil {
		return fmt.Errorf("failed to discover Console CRD presence: %w", err)
	}

	var consoleClient consoleclient.Interface
	if consoleAvailable {
		consoleClient, err = consoleclient.NewForConfig(cc.KubeConfig)
		if err != nil {
			return err
		}
	}

	certManagerControllerSet := certmanager.NewCertManagerControllerSet(
		kubeClient,
		kubeInformersForNamespaces,
		kubeInformersForNamespaces.InformersFor(operatorclient.TargetNamespace),
		*optInfraInformer,
		operatorClient,
		certManagerInformers,
		resourceapply.NewKubeClientHolder(kubeClient).WithAPIExtensionsClient(apiExtensionsClient),
		consoleClient,
		cc.EventRecorder,
		status.VersionForOperandFromEnv(),
		versionRecorder,
		TrustedCAConfigMapName,
		CloudCredentialSecret,
	)
	controllersToStart := certManagerControllerSet.ToArray()

	defaultCertManagerController := certmanager.NewDefaultCertManagerController(
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

	featureStatus, err := setupFeatureGates(ctx, configClient)
	if err != nil {
		return err
	}
	istioCSREnabled := features.IsIstioCSRFeatureGateEnabled()
	trustManagerEnabled := featureStatus.IsTrustManagerFeatureGateEnabled()

	if istioCSREnabled || trustManagerEnabled {
		// Create unified manager for all enabled operand controllers
		manager, err := NewControllerManager(ControllerConfig{
			EnableIstioCSR:     istioCSREnabled,
			EnableTrustManager: trustManagerEnabled,
		})
		if err != nil {
			return fmt.Errorf("failed to create unified controller manager: %w", err)
		}

		go func() {
			if err := manager.Start(ctx); err != nil {
				ctrl.Log.Error(err, "failed to start unified controller manager")
			}
		}()
	}

	<-ctx.Done()
	return nil
}

// startTLSSecurityProfileWatcher restarts the process when the cluster TLS
// profile or tlsAdherence changes. The metrics HTTPS server is configured only
// at process start, so it cannot pick up either change in place.
func startTLSSecurityProfileWatcher(ctx context.Context, configClient configv1client.Interface, tlsWatch OperatorTLSWatch) error {
	if !tlsWatch.Enabled {
		return nil
	}

	configInformers := configinformers.NewSharedInformerFactory(configClient, resyncInterval)
	tlsWatcher := &tlsprofile.SecurityProfileWatcher{
		InitialTLSProfileSpec:     tlsWatch.Resolved.Spec,
		InitialTLSAdherencePolicy: tlsWatch.Resolved.Adherence,
		InitialUnresolvable:       tlsWatch.Resolved.Unresolvable,
		OnChange: func() {
			// HTTPS cannot be reconfigured in place. RequestShutdown follows the
			// SIGTERM path already wired in the start command; os.Exit(0) is only
			// a fallback if that signal handler is missing.
			klog.Info("TLS security profile or adherence changed; requesting graceful operator restart")
			if !apiserver.RequestShutdown() {
				klog.Warning("failed to request a graceful shutdown, exiting directly")
				os.Exit(0)
			}
		},
	}
	if err := tlsWatcher.Start(configInformers.Config().V1().APIServers()); err != nil {
		return fmt.Errorf("failed to start TLS security profile watcher: %w", err)
	}
	go configInformers.Start(ctx.Done())
	return nil
}

// setupFeatureGates applies operator feature flags from UnsupportedAddonFeatures
// (--unsupported-addon-features) and builds cluster-side FeatureGateState by reading
// featuregates/cluster when served. Transient discovery or featuregates/cluster read errors
// are retried up to three times with 30s backoff; persistent failure leaves stateErr set
// on FeatureGateState without aborting the rest of startup. Returns an error only for invalid
// addon feature syntax or if ctx is canceled while waiting between retries.
func setupFeatureGates(ctx context.Context, configClient configv1client.Interface) (*features.FeatureGateState, error) {
	const (
		// maxFeatureGateAttempts caps how many times we call NewFeatureGateState when discovery or
		// featuregates/cluster read fails (excluding ErrNilConfigClient, which is not retried).
		maxFeatureGateAttempts = 3
		featureGateRetryDelay  = 30 * time.Second
	)

	var featureStatus *features.FeatureGateState

	if err := features.SetupWithFlagValue(UnsupportedAddonFeatures); err != nil {
		return nil, fmt.Errorf("failed to parse addon features: %w", err)
	}

	for attempt := 1; attempt <= maxFeatureGateAttempts; attempt++ {
		featureStatus = features.NewFeatureGateState(ctx, configClient)
		err := featureStatus.Err()
		if err == nil {
			break
		}
		if attempt == maxFeatureGateAttempts {
			ctrl.Log.Info("feature gate validation failed after max attempts; continuing with last state",
				"error", err, "attempts", attempt)
			break
		}
		ctrl.Log.Info("failed to validate featuregate settings, retrying",
			"error", err, "attempt", attempt, "maxAttempts", maxFeatureGateAttempts)
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("context cancelled: %w", ctx.Err())
		case <-time.After(featureGateRetryDelay):
		}
	}

	return featureStatus, nil
}
