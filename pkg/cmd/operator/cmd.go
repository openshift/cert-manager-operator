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
		operator.RunOperator,
	).NewCommandWithContext(context.TODO())
	cmd.Use = "start"
	cmd.Short = "Start the cert-manager Operator"
	cmd.Flags().StringVar(&operator.TrustedCAConfigMapName, "trusted-ca-configmap", "", "The name of the config map containing TLS CA(s) which should be trusted by the controller's containers. PEM encoded file under \"ca-bundle.crt\" key is expected.")
	cmd.Flags().StringVar(&operator.CloudCredentialSecret, "cloud-credentials-secret", "", "The name of the secret containing cloud credentials for authenticating using cert-manager ambient credentials mode.")
	return cmd
}
