// Package spiffeclient helps add-on agents on clusters with the spiffe add-on driver connect to
// the hub with their own SPIRE-issued SVID.
package spiffeclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc"
)

// Keys of the <addon>-spiffe ConfigMap the registration agent writes. The add-on manager mounts
// it where the hub kubeconfig Secret used to be.
const (
	KeyHubEndpoint       = "hub-endpoint"
	KeyHubSPIFFEID       = "hub-spiffe-id"
	KeyAgentSPIFFEID     = "agent-spiffe-id"
	KeyWorkloadAPISocket = "workload-api-socket"
)

// IsSPIFFEConfigDir reports whether dir holds the SPIFFE settings rather than a hub kubeconfig.
func IsSPIFFEConfigDir(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, KeyAgentSPIFFEID))
	return err == nil
}

// LoadConfig reads the SPIFFE settings mounted in dir.
func LoadConfig(dir string) (grpcoptions.SPIFFEConfig, error) {
	read := func(key string) (string, error) {
		b, err := os.ReadFile(filepath.Join(dir, key))
		if err != nil {
			return "", fmt.Errorf("SPIFFE setting %s: %w", key, err)
		}
		v := strings.TrimSpace(string(b))
		if v == "" {
			return "", fmt.Errorf("SPIFFE setting %s is empty", key)
		}
		return v, nil
	}
	var c grpcoptions.SPIFFEConfig
	var errs []error
	for key, dst := range map[string]*string{
		KeyHubEndpoint: &c.URL, KeyHubSPIFFEID: &c.HubSPIFFEID,
		KeyAgentSPIFFEID: &c.AgentSPIFFEID, KeyWorkloadAPISocket: &c.WorkloadAPISocket,
	} {
		v, err := read(key)
		errs = append(errs, err)
		*dst = v
	}
	return c, errors.Join(errs...)
}

// NewGRPCOptions reads the settings in dir and opens the Workload API. The returned closer stops
// SVID rotation; keep it open as long as the agent talks to the hub. The SVID stays in memory.
func NewGRPCOptions(ctx context.Context, dir string) (*grpcoptions.GRPCOptions, io.Closer, error) {
	c, err := LoadConfig(dir)
	if err != nil {
		return nil, nil, err
	}
	return grpcoptions.NewSPIFFEGRPCOptions(ctx, c)
}
