package main

import (
	"context"
	"github.com/openshift/ibm-powervs-block-csi-driver-operator/pkg/operator"
	"os"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	"k8s.io/component-base/cli"

	"github.com/openshift/ibm-powervs-block-csi-driver-operator/pkg/version"
)

func main() {
	command := NewOperatorCommand()
	code := cli.Run(command)
	os.Exit(code)
}

var guestKubeconfig *string

func NewOperatorCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "ibm-powervs-block-csi-driver-operator",
		Short: "OpenShift IBM PowerVS Block CSI Driver Operator",
		Run: func(cmd *cobra.Command, args []string) {
			cmd.Help()
			os.Exit(1)
		},
	}

	ctrlCmd := controllercmd.NewControllerCommandConfig(
		"ibm-powervs-block-csi-driver-operator",
		version.Get(),
		runOperatorWithGuestKubeconfig,
	).NewCommand()

	guestKubeconfig = ctrlCmd.Flags().String("guest-kubeconfig", "", "Path to the guest kubeconfig file. This flag enables hypershift integration.")

	ctrlCmd.Use = "start"
	ctrlCmd.Short = "Start the IBM PowerVS Block CSI Driver Operator"

	cmd.AddCommand(ctrlCmd)

	return cmd
}

func runOperatorWithGuestKubeconfig(ctx context.Context, controllerConfig *controllercmd.ControllerContext) error {
	return operator.RunOperator(ctx, controllerConfig, *guestKubeconfig)
}
