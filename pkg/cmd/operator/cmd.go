package operator

import (
	"context"
	"math/rand"
	"os"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apiserver/pkg/server"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"
	"k8s.io/utils/clock"

	"github.com/openshift/cert-manager-operator/pkg/operator"
	"github.com/openshift/cert-manager-operator/pkg/tlsprofile"
	"github.com/openshift/cert-manager-operator/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/openshift/library-go/pkg/controller/fileobserver"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/serviceability"
)

func NewOperator() *cobra.Command {
	cc := controllercmd.NewControllerCommandConfig(
		"cert-manager-operator",
		version.Get(),
		operator.RunOperator,
		clock.RealClock{},
	)

	cmd := cc.NewCommandWithContext(context.TODO())
	cmd.Use = "start"
	cmd.Short = "Start the cert-manager Operator"

	// Replace the default Run so we can apply the cluster TLS profile to the
	// metrics serving config before the HTTPS listener is created.
	cmd.Run = func(cmd *cobra.Command, args []string) {
		rand.Seed(time.Now().UTC().UnixNano())
		logs.InitLogs()
		defer logs.FlushLogs()
		defer serviceability.BehaviorOnPanic(os.Getenv("OPENSHIFT_ON_PANIC"), version.Get())()
		defer serviceability.Profile(os.Getenv("OPENSHIFT_PROFILE")).Stop()
		serviceability.StartProfiler()

		shutdownCtx, cancel := context.WithCancel(context.Background())
		shutdownHandler := server.SetupSignalHandler()
		go func() {
			defer cancel()
			<-shutdownHandler
			klog.Infof("Received SIGTERM or SIGINT signal, shutting down controller.")
		}()

		ctx, terminate := context.WithCancel(shutdownCtx)
		defer terminate()

		terminateOnFiles, err := cmd.Flags().GetStringArray("terminate-on-files")
		if err != nil {
			klog.Fatal(err)
		}
		if len(terminateOnFiles) > 0 {
			obs, err := fileobserver.NewObserver(10 * time.Second)
			if err != nil {
				klog.Fatal(err)
			}
			files := map[string][]byte{}
			for _, fn := range terminateOnFiles {
				fileBytes, err := os.ReadFile(fn)
				if err != nil {
					klog.Warningf("Unable to read initial content of %q: %v", fn, err)
					continue
				}
				files[fn] = fileBytes
			}
			obs.AddReactor(func(filename string, action fileobserver.ActionType) error {
				klog.Infof("exiting because %q changed", filename)
				terminate()
				return nil
			}, files, terminateOnFiles...)
			go obs.Run(shutdownHandler)
		}

		if err := startControllerWithClusterTLS(ctx, cc, cmd); err != nil {
			klog.Fatal(err)
		}
	}

	cmd.Flags().StringVar(&operator.TrustedCAConfigMapName, "trusted-ca-configmap", "", "The name of the config map containing TLS CA(s) which should be trusted by the controller's containers. PEM encoded file under \"ca-bundle.crt\" key is expected.")
	cmd.Flags().StringVar(&operator.CloudCredentialSecret, "cloud-credentials-secret", "", "The name of the secret containing cloud credentials for authenticating using cert-manager ambient credentials mode.")

	cmd.Flags().StringVar(&operator.UnsupportedAddonFeatures, "unsupported-addon-features", "",
		`List of unsupported addon features that the operator optionally enables.
		
eg. --unsupported-addon-features="IstioCSR=true"
		
Note: Technology Preview features are not supported with Red Hat production service level agreements (SLAs)
and might not be functionally complete. Red Hat does not recommend using them in production.

These features provide early access to upcoming product features,
enabling customers to test functionality and provide feedback during the development process.`)
	return cmd
}

func startControllerWithClusterTLS(ctx context.Context, c *controllercmd.ControllerCommandConfig, cmd *cobra.Command) error {
	unstructuredConfig, config, configContent, err := c.Config()
	if err != nil {
		return err
	}

	startingFileContent, observedFiles, err := c.AddDefaultRotationToConfig(config, configContent)
	if err != nil {
		return err
	}

	listen, err := cmd.Flags().GetString("listen")
	if err != nil {
		return err
	}
	if len(listen) != 0 {
		config.ServingInfo.BindAddress = listen
	}

	kubeConfigFile, err := cmd.Flags().GetString("kubeconfig")
	if err != nil {
		return err
	}
	namespace, err := cmd.Flags().GetString("namespace")
	if err != nil {
		return err
	}

	if !c.DisableServing {
		restConfig, err := tlsprofile.RESTConfigFromKubeConfig(kubeConfigFile)
		if err != nil {
			klog.Warningf("unable to build rest config for cluster TLS profile lookup; using Controllercmd default TLS settings: %v", err)
		} else {
			lookupCtx, cancelLookup := context.WithTimeout(ctx, 30*time.Second)
			err := tlsprofile.ApplyClusterProfileToHTTPServingInfo(lookupCtx, restConfig, &config.ServingInfo)
			cancelLookup()
			if err != nil {
				return err
			}
		}
	}

	exitOnChangeReactorCh := make(chan struct{})
	controllerCtx, cancel := context.WithCancel(ctx)
	go func() {
		select {
		case <-exitOnChangeReactorCh:
			cancel()
		case <-ctx.Done():
			cancel()
		}
	}()

	config.LeaderElection.Disable = c.DisableLeaderElection
	config.LeaderElection.LeaseDuration = c.LeaseDuration
	config.LeaderElection.RenewDeadline = c.RenewDeadline
	config.LeaderElection.RetryPeriod = c.RetryPeriod

	builder := controllercmd.NewController("cert-manager-operator", operator.RunOperator, clock.RealClock{}).
		WithKubeConfigFile(kubeConfigFile, nil).
		WithComponentNamespace(namespace).
		WithLeaderElection(config.LeaderElection, namespace, "cert-manager-operator-lock").
		WithVersion(version.Get()).
		WithEventRecorderOptions(events.RecommendedClusterSingletonCorrelatorOptions()).
		WithRestartOnChange(exitOnChangeReactorCh, startingFileContent, observedFiles...)

	if !c.DisableServing {
		builder = builder.WithServer(config.ServingInfo, config.Authentication, config.Authorization)
		if c.EnableHTTP2 {
			builder = builder.WithHTTP2()
		}
		if c.SkipInClusterAuthenticationLookup {
			builder = builder.WithSkipInClusterAuthenticationLookup()
		}
	}

	if c.TopologyDetector != nil {
		builder = builder.WithTopologyDetector(c.TopologyDetector)
	}

	return builder.Run(controllerCtx, unstructuredConfig)
}
