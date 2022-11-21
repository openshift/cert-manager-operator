package main

import (
	goflag "flag"
	"fmt"
	"math/rand"
	"os"
	"time"

	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"

	"github.com/openshift/cert-manager-operator/pkg/cmd/operator"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.InitLogs()
	defer logs.FlushLogs()

	command := NewOperatorCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
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
