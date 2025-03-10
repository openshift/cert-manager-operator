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

	cmd.Flags().StringVar(&operator.UnsupportedAddonFeatures, "unsupported-addon-features", "",
		`List of unsupported addon features that the operator optionally enables.
		
eg. --unsupported-addon-features="IstioCSR=true"
		
Note: Technology Preview features are not supported with Red Hat production service level agreements (SLAs)
and might not be functionally complete. Red Hat does not recommend using them in production.

These features provide early access to upcoming product features,
enabling customers to test functionality and provide feedback during the development process.`)
	return cmd
}
