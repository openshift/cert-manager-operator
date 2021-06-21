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
		"openshift-cert-manager-operator",
		version.Get(),
		operator.RunOperator,
	).NewCommandWithContext(context.TODO())
	cmd.Use = "start"
	cmd.Short = "Start the cert-manager Operator"
	return cmd
}
