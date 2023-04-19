package operator

import (
	"context"

	"github.com/openshift/cert-manager-operator/pkg/operator"
	"github.com/openshift/cert-manager-operator/pkg/version"
	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/spf13/cobra"
)

func NewOperator() *cobra.Command {
	cmd := controllercmd.NewControllerCommandConfig(
		"cert-manager-operator",
		version.Get(),
		operator.Run,
	).NewCommandWithContext(context.TODO())
	cmd.Use = "start"
	cmd.Short = "Start the cert-manager Operator"
	cmd.Flags().StringVar(&operator.TrustedCAConfigMapName, "trusted-ca-configmap", "", "The name of the config map containing TLS CA(s) which should be trusted by the controller's containers. PEM encoded file under \"ca-bundle.crt\" key is expected.")

	cmd.Flags().StringVar(&operator.MetricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&operator.ProbeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&operator.EnableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	// cmd.Flags().StringVar(&operator.Namespace, "namespace", "cert-manager-operator", "The namespace where operands should be installed")
	return cmd
}
