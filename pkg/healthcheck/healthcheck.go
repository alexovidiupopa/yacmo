// Package healthcheck provides pre/post-experiment health verification.
// It probes target endpoints before and after chaos to measure the blast radius
// and verify system recovery.
package healthcheck

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// Result holds the outcome of a single health probe.
type Result struct {
	Name       string        `json:"name"`
	URL        string        `json:"url"`
	Healthy    bool          `json:"healthy"`
	StatusCode int           `json:"status_code"`
	Latency    time.Duration `json:"latency"`
	Error      string        `json:"error,omitempty"`
}

// Checker performs health check probes against configured endpoints.
type Checker struct {
	cfg    config.HealthCheckConfig
	log    *logger.Logger
	client *http.Client
}

// New creates a new health checker.
func New(cfg config.HealthCheckConfig, log *logger.Logger) *Checker {
	timeout := time.Duration(cfg.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	return &Checker{
		cfg: cfg,
		log: log,
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// RunAll probes all configured endpoints and returns the results.
func (c *Checker) RunAll(ctx context.Context, phase string) []Result {
	if !c.cfg.Enabled || len(c.cfg.Endpoints) == 0 {
		return nil
	}

	c.log.Info("🩺 Running health checks (%s)...", phase)
	var results []Result

	for _, ep := range c.cfg.Endpoints {
		r := c.probe(ctx, ep)
		status := "✓ healthy"
		if !r.Healthy {
			status = "✗ unhealthy"
		}
		c.log.Info("  %s %s (%s) — %d, %s", status, r.Name, r.URL, r.StatusCode, r.Latency)
		results = append(results, r)
	}

	return results
}

// CompareResults compares pre- and post-chaos health results and logs the delta.
func (c *Checker) CompareResults(before, after []Result) {
	if len(before) == 0 || len(after) == 0 {
		return
	}

	c.log.Info("🩺 Health check comparison (before → after):")

	beforeMap := make(map[string]Result)
	for _, r := range before {
		beforeMap[r.Name] = r
	}

	degraded := 0
	recovered := 0
	for _, a := range after {
		b, ok := beforeMap[a.Name]
		if !ok {
			continue
		}

		var status string
		switch {
		case b.Healthy && !a.Healthy:
			status = "🔴 DEGRADED"
			degraded++
		case !b.Healthy && a.Healthy:
			status = "🟢 RECOVERED"
			recovered++
		case b.Healthy && a.Healthy:
			status = "🟢 OK"
		default:
			status = "🔴 STILL DOWN"
			degraded++
		}

		latencyDelta := a.Latency - b.Latency
		c.log.Info("  %s %s — latency %s → %s (Δ %s)",
			status, a.Name, b.Latency, a.Latency, formatDelta(latencyDelta))
	}

	c.log.Info("  Summary: %d degraded, %d recovered, %d stable",
		degraded, recovered, len(after)-degraded-recovered)
}

// probe checks a single endpoint.
func (c *Checker) probe(ctx context.Context, ep config.HealthEndpoint) Result {
	r := Result{
		Name: ep.Name,
		URL:  ep.URL,
	}

	method := strings.ToUpper(ep.Method)
	if method == "" {
		method = http.MethodGet
	}

	req, err := http.NewRequestWithContext(ctx, method, ep.URL, nil)
	if err != nil {
		r.Error = err.Error()
		return r
	}

	start := time.Now()
	resp, err := c.client.Do(req)
	r.Latency = time.Since(start)

	if err != nil {
		r.Error = err.Error()
		return r
	}
	defer resp.Body.Close()

	r.StatusCode = resp.StatusCode

	expectedStatus := ep.ExpectedStatus
	if expectedStatus == 0 {
		expectedStatus = 200
	}
	r.Healthy = resp.StatusCode == expectedStatus

	return r
}

func formatDelta(d time.Duration) string {
	if d >= 0 {
		return fmt.Sprintf("+%s", d)
	}
	return d.String()
}
