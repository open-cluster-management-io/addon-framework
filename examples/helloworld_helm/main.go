package main

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"time"

	goflag "flag"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	utilrand "k8s.io/apimachinery/pkg/util/rand"
	utilflag "k8s.io/component-base/cli/flag"
	"k8s.io/component-base/logs"
	"k8s.io/klog/v2"

	helloworldagent "open-cluster-management.io/addon-framework/examples/helloworld/agent"
	"open-cluster-management.io/addon-framework/examples/helloworld_helm/cleanup_agent"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/version"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
)

const (
	addonName = "helloworldhelm"
)

func main() {
	rand.Seed(time.Now().UTC().UnixNano())

	pflag.CommandLine.SetNormalizeFunc(utilflag.WordSepNormalizeFunc)
	pflag.CommandLine.AddGoFlagSet(goflag.CommandLine)

	logs.NewOptions().AddFlags(pflag.CommandLine)

	command := newCommand()
	if err := command.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func newCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "addon",
		Short: "helloworldhelm example addon",
		Run: func(cmd *cobra.Command, args []string) {
			if err := cmd.Help(); err != nil {
				fmt.Fprintf(os.Stderr, "%v\n", err)
			}
			os.Exit(1)
		},
	}

	if v := version.Get().String(); len(v) == 0 {
		cmd.Version = "<unknown>"
	} else {
		cmd.Version = v
	}

	cmd.AddCommand(newControllerCommand())
	cmd.AddCommand(helloworldagent.NewAgentCommand(addonName))
	cmd.AddCommand(cleanup_agent.NewAgentCommand(addonName))
	return cmd
}

func newControllerCommand() *cobra.Command {
	cmdConfig := controllercmd.NewControllerCommandConfig("helloworldhelm-addon-controller", version.Get(), runController)
	cmdConfig.DisableLeaderElection = false

	cmd := cmdConfig.NewCommand()
	cmd.Use = "controller"
	cmd.Short = "Start the addon controller"
	return cmd
}

func runController(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return err
	}

	mgr, err := addonmanager.New(controllerContext.KubeConfig)
	if err != nil {
		klog.Errorf("failed to new addon manager %v", err)
		return err
	}

	registrationOption := newRegistrationOption(
		controllerContext.KubeConfig,
		controllerContext.EventRecorder,
		utilrand.String(5))

	agentAddon, err := addonfactory.NewAgentAddonFactory(addonName, FS, "manifests/charts/helloworld").
		WithGetValuesFuncs(getValuesFromAddOnDeploymentConfig(addonClient)).
		WithAgentRegistrationOption(registrationOption).
		BuildHelmAgentAddon()
	if err != nil {
		klog.Errorf("failed to build agent %v", err)
		return err
	}

	err = mgr.AddAgent(agentAddon)
	if err != nil {
		klog.Fatal(err)
	}

	err = mgr.Start(ctx)
	if err != nil {
		klog.Fatal(err)
	}
	<-ctx.Done()

	return nil
}
