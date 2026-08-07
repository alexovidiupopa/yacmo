package healthcheck_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"yacmo/pkg/config"
	"yacmo/pkg/healthcheck"
	"yacmo/pkg/logger"
)

func testLogger() *logger.Logger { return logger.New("error") }

func TestRunAllDisabled(t *testing.T) {
	c := healthcheck.New(config.HealthCheckConfig{Enabled: false}, testLogger())
	if got := c.RunAll(context.Background(), "pre"); got != nil {
		t.Errorf("RunAll should return nil when disabled, got %v", got)
	}
}

func TestProbeHealthy(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.HealthCheckConfig{
		Enabled:        true,
		TimeoutSeconds: 5,
		Endpoints: []config.HealthEndpoint{
			{Name: "ok", URL: srv.URL, ExpectedStatus: 200},
		},
	}
	c := healthcheck.New(cfg, testLogger())
	results := c.RunAll(context.Background(), "pre")

	if len(results) != 1 {
		t.Fatalf("got %d results, want 1", len(results))
	}
	if !results[0].Healthy {
		t.Errorf("endpoint should be healthy, got %+v", results[0])
	}
	if results[0].StatusCode != 200 {
		t.Errorf("StatusCode = %d, want 200", results[0].StatusCode)
	}
}

func TestProbeUnhealthyOnStatusMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	cfg := config.HealthCheckConfig{
		Enabled:        true,
		TimeoutSeconds: 5,
		Endpoints: []config.HealthEndpoint{
			{Name: "down", URL: srv.URL}, // ExpectedStatus defaults to 200
		},
	}
	c := healthcheck.New(cfg, testLogger())
	results := c.RunAll(context.Background(), "post")

	if len(results) != 1 || results[0].Healthy {
		t.Fatalf("endpoint should be unhealthy, got %+v", results)
	}
}

func TestProbeConnectionError(t *testing.T) {
	cfg := config.HealthCheckConfig{
		Enabled:        true,
		TimeoutSeconds: 1,
		Endpoints: []config.HealthEndpoint{
			// Reserved TEST-NET address / closed port -> connection error.
			{Name: "unreachable", URL: "http://127.0.0.1:1"},
		},
	}
	c := healthcheck.New(cfg, testLogger())
	results := c.RunAll(context.Background(), "pre")

	if len(results) != 1 || results[0].Healthy {
		t.Fatalf("unreachable endpoint should be unhealthy, got %+v", results)
	}
	if results[0].Error == "" {
		t.Error("expected a connection error to be recorded")
	}
}

func TestCompareResultsNoPanic(t *testing.T) {
	c := healthcheck.New(config.HealthCheckConfig{Enabled: true}, testLogger())
	before := []healthcheck.Result{{Name: "a", Healthy: true}}
	after := []healthcheck.Result{{Name: "a", Healthy: false}}
	// Should not panic; primarily a smoke test of the comparison logic.
	c.CompareResults(before, after)
}
