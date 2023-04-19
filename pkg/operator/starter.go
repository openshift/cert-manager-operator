package operator

import (
	"context"
	"time"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/status"
	"github.com/openshift/library-go/pkg/operator/v1helpers"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/openshift/cert-manager-operator/api/operator/v1alpha1"
	"github.com/openshift/cert-manager-operator/pkg/controller/deployment"
	certmanoperatorclient "github.com/openshift/cert-manager-operator/pkg/operator/clientset/versioned"
	certmanoperatorinformers "github.com/openshift/cert-manager-operator/pkg/operator/informers/externalversions"
	"github.com/openshift/cert-manager-operator/pkg/operator/operatorclient"
)

const (
	resyncInterval = 10 * time.Minute
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	//+kubebuilder:scaffold:scheme

	utilruntime.Must(v1alpha1.AddToScheme(scheme))

}

var (
	scheme = runtime.NewScheme()

	MetricsAddr          string
	EnableLeaderElection bool
	ProbeAddr            string
	Namespace            string

	// TrustedCAConfigMapName is the trusted ca configmap name
	// provided as a runtime arg.
	TrustedCAConfigMapName string
)

func Run(ctx context.Context, cc *controllercmd.ControllerContext) error {

	log.SetLogger(klog.FromContext(ctx))

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
		"cert-manager",
		operatorclient.TargetNamespace,
	)
	certManagerControllerSet := deployment.NewCertManagerControllerSet(
		kubeClient,
		kubeInformersForNamespaces,
		kubeInformersForNamespaces.InformersFor(operatorclient.TargetNamespace),
		operatorClient,
		certManagerInformers,
		resourceapply.NewKubeClientHolder(kubeClient).WithAPIExtensionsClient(apiExtensionsClient),
		cc.EventRecorder,
		status.VersionForOperandFromEnv(),
		versionRecorder,
		TrustedCAConfigMapName,
	)
	_ = certManagerControllerSet.ToArray()

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme:                 scheme,
		MetricsBindAddress:     MetricsAddr,
		Port:                   9443,
		HealthProbeBindAddress: ProbeAddr,
		LeaderElection:         EnableLeaderElection,
		LeaderElectionID:       "7de51cf3.openshift.io",
		SyncPeriod:             pointer.Duration(5 * time.Second),
		// The default cached client does not always return an updated value after write operations. So we use a non-cache client
		// https://pkg.go.dev/sigs.k8s.io/controller-runtime#hdr-Clients_and_Caches
		NewClient: func(_ cache.Cache, config *rest.Config, options client.Options, _ ...client.Object) (client.Client, error) {
			return client.New(config, options)
		},
		Namespace: Namespace,
	})
	if err != nil {
		return err
	}

	if err = (&deployment.CertManagerReconciler{
		Client:                     mgr.GetClient(),
		Scheme:                     mgr.GetScheme(),
		Recorder:                   cc.EventRecorder,
		OperatorClient:             operatorClient,
		CertManagerClient:          certManagerOperatorClient.OperatorV1alpha1(),
		ControllerSet:              certManagerControllerSet,
		CertManagerInformers:       certManagerInformers,
		KubeInformersForNamespaces: kubeInformersForNamespaces,
	}).SetupWithManager(mgr); err != nil {
		return err
	}

	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		return err
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		return err
	}

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		return err
	}

	return nil

}

// func RunOperator(ctx context.Context, cc *controllercmd.ControllerContext) error {
// 	kubeClient, err := kubernetes.NewForConfig(cc.ProtoKubeConfig)
// 	if err != nil {
// 		return err
// 	}
//
// 	certManagerOperatorClient, err := certmanoperatorclient.NewForConfig(cc.KubeConfig)
// 	if err != nil {
// 		return err
// 	}
//
// 	apiExtensionsClient, err := apiextensionsclient.NewForConfig(cc.KubeConfig)
// 	if err != nil {
// 		return err
// 	}
//
// 	certManagerInformers := certmanoperatorinformers.NewSharedInformerFactory(certManagerOperatorClient, resyncInterval)
//
// 	operatorClient := &operatorclient.OperatorClient{
// 		Informers: certManagerInformers,
// 		Client:    certManagerOperatorClient.OperatorV1alpha1(),
// 	}
//
// 	// perform version changes to the version getter prior to tying it up in the status controller
// 	// via change-notification channel so that it only updates operator version in status once
// 	// either of the workloads synces
// 	versionRecorder := status.NewVersionGetter()
// 	versionRecorder.SetVersion("operator", status.VersionForOperatorFromEnv())
//
// 	kubeInformersForNamespaces := v1helpers.NewKubeInformersForNamespaces(kubeClient,
// 		"",
// 		"kube-system",
// 		"cert-manager",
// 		operatorclient.TargetNamespace,
// 	)
// 	certManagerControllerSet := deployment.NewCertManagerControllerSet(
// 		kubeClient,
// 		kubeInformersForNamespaces,
// 		kubeInformersForNamespaces.InformersFor(operatorclient.TargetNamespace),
// 		operatorClient,
// 		certManagerInformers,
// 		resourceapply.NewKubeClientHolder(kubeClient).WithAPIExtensionsClient(apiExtensionsClient),
// 		cc.EventRecorder,
// 		status.VersionForOperandFromEnv(),
// 		versionRecorder,
// 		TrustedCAConfigMapName,
// 	)
// 	controllersToStart := certManagerControllerSet.ToArray()
//
// 	// defaultCertManagerController := deployment.NewDefaultCertManagerController(
// 	// 	operatorClient,
// 	// 	certManagerOperatorClient.OperatorV1alpha1(),
// 	// 	cc.EventRecorder,
// 	// )
//
// 	// controllersToStart = append(controllersToStart, defaultCertManagerController)
//
// 	for _, informer := range []interface{ Start(<-chan struct{}) }{
// 		certManagerInformers,
// 		kubeInformersForNamespaces,
// 	} {
// 		informer.Start(ctx.Done())
// 	}
//
// 	for _, controller := range controllersToStart {
// 		go controller.Run(ctx, 1)
// 	}
//
// 	<-ctx.Done()
// 	return nil
// }
