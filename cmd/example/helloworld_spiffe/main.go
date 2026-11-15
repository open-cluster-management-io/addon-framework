// helloworld_spiffe runs the hub-side manager ("controller") and the agent ("agent") of the
// helloworld-spiffe example add-on.
package main

import (
	"context"
	"errors"
	goflag "flag"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	utilflag "k8s.io/component-base/cli/flag"
	logs "k8s.io/component-base/logs/api/v1"

	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe"
	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe_agent"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/version"
)

// exit, newAddOnManager and newAgentAddon are replaceable in tests.
var (
	exit            = os.Exit
	newAddOnManager = addonmanager.New
	newAgentAddon   = helloworld_spiffe.NewAgentAddon
)

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	logs.AddFlags(logs.NewLoggingConfiguration(), pflag.CommandLine)
	exit(run(newCommand(), os.Stderr))
}

// run runs the command and returns the process exit code.
func run(cmd *cobra.Command, stderr io.Writer) int {
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(stderr, "%v\n", err)
		return 1
	}
	return 0
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "helloworld_spiffe",
		Short: "helloworld-spiffe example add-on",
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = cmd.Help()
			return errors.New("a command is required: controller or agent")
		},
		SilenceUsage: true,
	}
	controller := cmdfactory.NewControllerCommandConfig("helloworld-spiffe-addon-controller", version.Get(), runController).
		NewCommand()
	controller.Use = "controller"
	controller.Short = "Start the add-on manager on the hub"
	cmd.AddCommand(controller, helloworld_spiffe_agent.NewAgentCommand())
	return cmd
}

func runController(ctx context.Context, kubeConfig *rest.Config) error {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	mgr, err := newAddOnManager(kubeConfig)
	if err != nil {
		return err
	}
	agentAddon, err := newAgentAddon(kubeClient)
	if err != nil {
		return err
	}
	if err := mgr.AddAgent(agentAddon); err != nil {
		return err
	}
	if err := mgr.Start(ctx); err != nil {
		return err
	}
	<-ctx.Done()
	return nil
}
