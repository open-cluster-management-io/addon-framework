package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"open-cluster-management.io/addon-framework/examples/helloworld_spiffe"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/agent"
)

func TestMainHelp(t *testing.T) {
	code := -1
	exit = func(c int) { code = c }
	defer func() { exit = os.Exit }()
	args := os.Args
	defer func() { os.Args = args }()
	// main registers its flags on the global FlagSet: give it a fresh one, so the test can run
	// again in the same process (go test -count=2).
	flags := pflag.CommandLine
	pflag.CommandLine = pflag.NewFlagSet(os.Args[0], pflag.ExitOnError)
	defer func() { pflag.CommandLine = flags }()
	os.Args = []string{"helloworld_spiffe", "--help"}
	main()
	if code != 0 {
		t.Errorf("exit code %d", code)
	}
}

func TestRunWithoutCommand(t *testing.T) {
	cmd := newCommand()
	cmd.SetArgs(nil)
	var out, stderr bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if code := run(cmd, &stderr); code != 1 || !strings.Contains(stderr.String(), "a command is required") {
		t.Errorf("code %d, stderr %q", code, stderr.String())
	}
	for _, name := range []string{"controller", "agent"} {
		if c, _, err := cmd.Find([]string{name}); err != nil || c.Name() != name {
			t.Errorf("no %s command: %v", name, err)
		}
	}
}

// fakeManager stands in for the add-on manager.
type fakeManager struct {
	addonmanager.AddonManager
	addErr, startErr error
	added            []agent.AgentAddon
}

func (m *fakeManager) AddAgent(a agent.AgentAddon) error {
	m.added = append(m.added, a)
	return m.addErr
}
func (m *fakeManager) Start(context.Context) error { return m.startErr }

func TestRunController(t *testing.T) {
	defer func() {
		newAddOnManager, newAgentAddon = addonmanager.New, helloworld_spiffe.NewAgentAddon
	}()
	failed := errors.New("failed")
	config := &rest.Config{Host: "https://hub.example.com"}

	cases := []struct {
		name    string
		config  *rest.Config
		manager func(*rest.Config) (addonmanager.AddonManager, error)
		addOn   func(kubernetes.Interface) (agent.AgentAddon, error)
		mgr     *fakeManager
		wantErr bool
	}{
		{name: "an unusable kubeconfig", config: &rest.Config{Host: "https://hub.example.com",
			TLSClientConfig: rest.TLSClientConfig{CAFile: "/nonexistent/ca.crt"}}, wantErr: true},
		{name: "the manager can't be built", config: config,
			manager: func(*rest.Config) (addonmanager.AddonManager, error) { return nil, failed }, wantErr: true},
		{name: "the add-on can't be built", config: config, mgr: &fakeManager{},
			addOn: func(kubernetes.Interface) (agent.AgentAddon, error) { return nil, failed }, wantErr: true},
		{name: "the add-on can't be added", config: config, mgr: &fakeManager{addErr: failed}, wantErr: true},
		{name: "the manager can't start", config: config, mgr: &fakeManager{startErr: failed}, wantErr: true},
		{name: "runs until stopped", config: config, mgr: &fakeManager{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			newAddOnManager, newAgentAddon = addonmanager.New, helloworld_spiffe.NewAgentAddon
			if c.manager != nil {
				newAddOnManager = c.manager
			} else if c.mgr != nil {
				newAddOnManager = func(*rest.Config) (addonmanager.AddonManager, error) { return c.mgr, nil }
			}
			if c.addOn != nil {
				newAgentAddon = c.addOn
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			err := runController(ctx, c.config)
			if (err != nil) != c.wantErr {
				t.Fatalf("error %v, want error %v", err, c.wantErr)
			}
			if !c.wantErr && len(c.mgr.added) != 1 {
				t.Errorf("added %d add-ons, want 1", len(c.mgr.added))
			}
		})
	}
}
