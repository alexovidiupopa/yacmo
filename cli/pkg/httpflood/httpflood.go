// Package httpflood provides HTTP traffic injection chaos experiments.
// It can generate floods of HTTP requests, inject latency, randomize payloads,
// and simulate error scenarios against target endpoints.
package httpflood

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// ChaosHTTP implements chaos.Experiment for HTTP traffic injection.
type ChaosHTTP struct {
	cfg    config.HTTPConfig
	log    *logger.Logger
	client *http.Client
}

// Stats holds aggregate statistics for an HTTP injection run.
type Stats struct {
	TotalRequests  int64
	SuccessCount   int64
	ErrorCount     int64
	TotalLatencyMs int64 // sum of all response times in ms
}

// New creates a new HTTP chaos experiment.
func New(cfg config.HTTPConfig, log *logger.Logger) *ChaosHTTP {
	return &ChaosHTTP{
		cfg: cfg,
		log: log,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the experiment name.
func (c *ChaosHTTP) Name() string {
	var names []string
	for _, t := range c.cfg.Targets {
		names = append(names, t.Name)
	}
	return fmt.Sprintf("http-flood[targets=%s]", strings.Join(names, ","))
}

// Run executes the HTTP traffic injection experiment.
func (c *ChaosHTTP) Run(ctx context.Context) error {
	for _, target := range c.cfg.Targets {
		c.log.Info("🌊 Starting HTTP injection against %s (%s %s)", target.Name, target.Method, target.URL)

		stats, err := c.flood(ctx, target)
		if err != nil {
			return fmt.Errorf("http flood %s: %w", target.Name, err)
		}

		avgLatency := float64(0)
		if stats.TotalRequests > 0 {
			avgLatency = float64(stats.TotalLatencyMs) / float64(stats.TotalRequests)
		}

		c.log.Info("📊 %s results: total=%d success=%d errors=%d avg_latency=%.1fms",
			target.Name, stats.TotalRequests, stats.SuccessCount, stats.ErrorCount, avgLatency)
	}
	return nil
}

// Rollback is a no-op for HTTP injection (traffic already sent).
func (c *ChaosHTTP) Rollback(_ context.Context) error {
	c.log.Info("HTTP injection rollback: nothing to undo (traffic already sent)")
	return nil
}

// flood sends concurrent HTTP requests to a single target.
func (c *ChaosHTTP) flood(ctx context.Context, target config.HTTPTarget) (*Stats, error) {
	concurrency := target.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	totalRequests := target.TotalRequests
	if totalRequests <= 0 {
		totalRequests = 100
	}

	timeout := time.Duration(target.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}

	client := &http.Client{Timeout: timeout}

	method := strings.ToUpper(target.Method)
	if method == "" {
		method = "GET"
	}

	var stats Stats

	// Create a context with duration limit if specified
	floodCtx := ctx
	if target.Duration > 0 {
		var cancel context.CancelFunc
		floodCtx, cancel = context.WithTimeout(ctx, target.Duration)
		defer cancel()
	}

	// Work channel and rate limiter
	work := make(chan int, concurrency)
	var rateTicker *time.Ticker
	if target.RatePerSecond > 0 {
		interval := time.Duration(float64(time.Second) / target.RatePerSecond)
		rateTicker = time.NewTicker(interval)
		defer rateTicker.Stop()
	}

	// Spawn workers
	var wg sync.WaitGroup
	for w := 0; w < concurrency; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range work {
				select {
				case <-floodCtx.Done():
					return
				default:
				}

				// Inject artificial latency if configured
				if target.InjectLatency > 0 {
					time.Sleep(target.InjectLatency)
				}

				// Build request body
				var body io.Reader
				if target.RandomizeBody && target.BodySizeBytes > 0 {
					body = bytes.NewReader(randomBytes(target.BodySizeBytes))
				} else if target.Body != "" {
					body = strings.NewReader(target.Body)
				}

				req, err := http.NewRequestWithContext(floodCtx, method, target.URL, body)
				if err != nil {
					atomic.AddInt64(&stats.ErrorCount, 1)
					atomic.AddInt64(&stats.TotalRequests, 1)
					continue
				}

				// Set headers
				for k, v := range target.Headers {
					req.Header.Set(k, v)
				}
				if req.Header.Get("Content-Type") == "" && body != nil {
					req.Header.Set("Content-Type", "application/octet-stream")
				}

				start := time.Now()
				resp, err := client.Do(req)
				elapsed := time.Since(start).Milliseconds()
				atomic.AddInt64(&stats.TotalLatencyMs, elapsed)
				atomic.AddInt64(&stats.TotalRequests, 1)

				if err != nil {
					atomic.AddInt64(&stats.ErrorCount, 1)
					c.log.Debug("Request error: %v", err)
					continue
				}
				// Drain and close body
				io.Copy(io.Discard, resp.Body)
				resp.Body.Close()

				if resp.StatusCode >= 400 {
					atomic.AddInt64(&stats.ErrorCount, 1)
				} else {
					atomic.AddInt64(&stats.SuccessCount, 1)
				}
			}
		}()
	}

	// Dispatch work
	go func() {
		defer close(work)
		for i := 0; i < totalRequests; i++ {
			select {
			case <-floodCtx.Done():
				return
			default:
			}

			if rateTicker != nil {
				select {
				case <-rateTicker.C:
				case <-floodCtx.Done():
					return
				}
			}

			select {
			case work <- i:
			case <-floodCtx.Done():
				return
			}
		}
	}()

	wg.Wait()
	return &stats, nil
}

// randomBytes generates n random bytes.
func randomBytes(n int) []byte {
	buf := make([]byte, n)
	rand.Read(buf)
	return buf
}
