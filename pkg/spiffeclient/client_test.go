package spiffeclient

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"open-cluster-management.io/sdk-go/pkg/testing/fakespire"
)

func TestLoadConfig(t *testing.T) {
	dir := t.TempDir()
	if IsSPIFFEConfigDir(dir) {
		t.Error("empty dir taken for SPIFFE settings")
	}
	for k, v := range map[string]string{KeyHubEndpoint: "hub:443", KeyHubSPIFFEID: "spiffe://td/ocm/hub/grpc-server",
		KeyAgentSPIFFEID: "spiffe://td/ocm/cluster/c1/addon/a/agent/a\n"} {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadConfig(dir); err == nil {
		t.Error("no error without the socket")
	}
	if err := os.WriteFile(filepath.Join(dir, KeyWorkloadAPISocket), []byte("unix:///s.sock"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !IsSPIFFEConfigDir(dir) || c.URL != "hub:443" || c.AgentSPIFFEID != "spiffe://td/ocm/cluster/c1/addon/a/agent/a" || c.WorkloadAPISocket != "unix:///s.sock" {
		t.Errorf("config %+v", c)
	}
}

func writeSettings(t *testing.T, settings map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for k, v := range settings {
		if err := os.WriteFile(filepath.Join(dir, k), []byte(v), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadConfigEmptySetting(t *testing.T) {
	dir := writeSettings(t, map[string]string{KeyHubEndpoint: "hub:443", KeyHubSPIFFEID: "spiffe://td/ocm/hub/grpc-server",
		KeyAgentSPIFFEID: "  \n", KeyWorkloadAPISocket: "unix:///s.sock"})
	if _, err := LoadConfig(dir); err == nil || !strings.Contains(err.Error(), "is empty") {
		t.Errorf("an empty agent ID: %v", err)
	}
}

func TestNewGRPCOptions(t *testing.T) {
	const agentID = "spiffe://ocm-hub.example.org/ocm/cluster/c1/addon/a/agent/a"
	ca := fakespire.NewCA(t, "ocm-hub.example.org")
	spire := fakespire.Start(t, ca)
	spire.SetSVIDs(ca.Issue(t, agentID, time.Hour))
	dir := writeSettings(t, map[string]string{KeyHubEndpoint: "hub:443",
		KeyHubSPIFFEID: "spiffe://ocm-hub.example.org/ocm/hub/grpc-server", KeyAgentSPIFFEID: agentID,
		KeyWorkloadAPISocket: spire.Addr})
	opts, closer, err := NewGRPCOptions(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer closer.Close()
	if opts.Dialer == nil || opts.Dialer.URL != "hub:443" || opts.Dialer.TLSConfig == nil {
		t.Errorf("dialer %+v", opts.Dialer)
	}

	if _, _, err := NewGRPCOptions(context.Background(), t.TempDir()); err == nil {
		t.Error("no error without settings")
	}
}
