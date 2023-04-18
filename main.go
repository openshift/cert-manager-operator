package main

import (
	goflag "flag"
	"math/rand"
	"os"
	"time"

	"k8s.io/component-base/cli"
	utilflag "k8s.io/component-base/cli/flag"

	"github.com/openshift/cert-manager-operator/pkg/cmd/operator"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	command := NewOperatorCommand()
	code := cli.Run(command)
	os.Exit(code)
}

func NewOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cert-manager-operator",
		Short: "OpenShift cluster cert-manager operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}
	cmd.AddCommand(operator.NewOperator())
	return cmd
}
