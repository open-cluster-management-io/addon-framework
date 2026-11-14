// helloworld_spiffe runs the hub-side manager ("controller") and the agent ("agent") of the
// helloworld-spiffe example add-on.
package main

import (
	"context"
	goflag "flag"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	utilflag "k8s.io/component-base/cli/flag"
	logs "k8s.io/component-base/logs/api/v1"

	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe"
	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe_agent"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	cmdfactory "open-cluster-management.io/addon-framework/pkg/cmd/factory"
	"open-cluster-management.io/addon-framework/pkg/version"
	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

func main() {
	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	logs.AddFlags(logs.NewLoggingConfiguration(), pflag.CommandLine)

	cmd := &cobra.Command{
		Use:   "helloworld_spiffe",
		Short: "helloworld-spiffe example add-on",
		Run: func(cmd *cobra.Command, args []string) {
			_ = cmd.Help()
			os.Exit(1)
		},
	}
	controller := cmdfactory.NewControllerCommandConfig("helloworld-spiffe-addon-controller", version.Get(), runController).
		NewCommand()
	controller.Use = "controller"
	controller.Short = "Start the add-on manager on the hub"
	cmd.AddCommand(controller, helloworld_spiffe_agent.NewAgentCommand())
	if err := cmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runController(ctx context.Context, kubeConfig *rest.Config) error {
	kubeClient, err := kubernetes.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}
	mgr, err := addonmanager.New(kubeConfig)
	if err != nil {
		return err
	}
	agentAddon, err := addonfactory.NewAgentAddonFactory(helloworld_spiffe.AddonName, helloworld_spiffe.FS, "manifests/templates").
		WithGetValuesFuncs(helloworld_spiffe.GetDefaultValues).
		WithAgentRegistrationOption(helloworld_spiffe.NewRegistrationOption(kubeClient)).
		WithAgentInstallNamespace(func(context.Context, *addonapiv1beta1.ManagedClusterAddOn) (string, error) {
			return helloworld_spiffe.InstallNamespace, nil
		}).
		WithAgentHealthProber(helloworld_spiffe.AgentHealthProber()).
		BuildTemplateAgentAddon()
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
