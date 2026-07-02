// Package metrics provides Prometheus metrics exposition for YACMO.
// It runs an HTTP server at /metrics that exposes counters and histograms
// for all chaos experiments.
package metrics

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"yacmo/pkg/logger"
)

// Collector tracks experiment metrics in-process.
// For a thesis project this avoids pulling in the full Prometheus client
// dependency while still exposing metrics in Prometheus text format.
type Collector struct {
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]float64
	log      *logger.Logger
}

// NewCollector creates a new metrics collector.
func NewCollector(log *logger.Logger) *Collector {
	return &Collector{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
		log:      log,
	}
}

// IncCounter increments a named counter by 1.
func (c *Collector) IncCounter(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name]++
}

// AddCounter adds a value to a named counter.
func (c *Collector) AddCounter(name string, val int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.counters[name] += val
}

// SetGauge sets a gauge to a specific value.
func (c *Collector) SetGauge(name string, val float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gauges[name] = val
}

// Snapshot returns a copy of all current metrics.
func (c *Collector) Snapshot() (map[string]int64, map[string]float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	counters := make(map[string]int64, len(c.counters))
	for k, v := range c.counters {
		counters[k] = v
	}
	gauges := make(map[string]float64, len(c.gauges))
	for k, v := range c.gauges {
		gauges[k] = v
	}
	return counters, gauges
}

// ServeHTTP implements http.Handler — renders metrics in Prometheus text format.
func (c *Collector) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	counters, gauges := c.Snapshot()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	for name, val := range counters {
		fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, val)
	}
	for name, val := range gauges {
		fmt.Fprintf(w, "# TYPE %s gauge\n%s %f\n", name, name, val)
	}
}

// Server runs the /metrics HTTP endpoint.
type Server struct {
	addr      string
	collector *Collector
	log       *logger.Logger
	srv       *http.Server
}

// NewServer creates a metrics server.
func NewServer(addr string, collector *Collector, log *logger.Logger) *Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", collector)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})

	return &Server{
		addr:      addr,
		collector: collector,
		log:       log,
		srv: &http.Server{
			Addr:    addr,
			Handler: mux,
		},
	}
}

// Start begins serving metrics in the background. Call Shutdown to stop.
func (s *Server) Start() {
	go func() {
		s.log.Info("📈 Metrics server listening on %s/metrics", s.addr)
		if err := s.srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.log.Error("Metrics server error: %v", err)
		}
	}()
}

// Shutdown gracefully stops the metrics server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.log.Info("Shutting down metrics server...")
	shutdownCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.srv.Shutdown(shutdownCtx)
}
