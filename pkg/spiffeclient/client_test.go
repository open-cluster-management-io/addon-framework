package spiffeclient

import (
	"os"
	"path/filepath"
	"testing"
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
