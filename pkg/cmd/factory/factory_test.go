package factory

import (
	"net/http"
	"net/http/httptest"
	"testing"

	genericapiserver "k8s.io/apiserver/pkg/server"
	utilversion "k8s.io/component-base/compatibility"
)

func TestToServerConfigDisablesProfilingAndMetrics(t *testing.T) {
	cfg, err := toServerConfig()
	if err != nil {
		t.Fatalf("toServerConfig() failed: %v", err)
	}
	if cfg.EnableProfiling {
		t.Fatal("expected EnableProfiling to be false")
	}
	if cfg.EnableMetrics {
		t.Fatal("expected EnableMetrics to be false")
	}

	cfg.EffectiveVersion = utilversion.NewEffectiveVersionFromString("v1.0.0", "", "")
	server, err := cfg.Complete(nil).New("test", genericapiserver.NewEmptyDelegate())
	if err != nil {
		t.Fatalf("failed to construct generic API server: %v", err)
	}
	prepared := server.PrepareRun()

	for _, path := range []string{"/debug/pprof/", "/metrics", "/metrics/slis"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		prepared.Handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 for %s, got %d", path, rec.Code)
		}
	}

	healthzReq := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	healthzRec := httptest.NewRecorder()
	prepared.Handler.ServeHTTP(healthzRec, healthzReq)
	if healthzRec.Code == http.StatusNotFound {
		t.Fatal("expected /healthz to be present, got 404")
	}
}
