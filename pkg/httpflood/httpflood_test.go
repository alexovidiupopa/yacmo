package httpflood_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/httpflood"
	"yacmo/pkg/logger"
)

func testLogger() *logger.Logger { return logger.New("error") }

func TestName(t *testing.T) {
	cfg := config.HTTPConfig{
		Targets: []config.HTTPTarget{{Name: "t1"}, {Name: "t2"}},
	}
	c := httpflood.New(cfg, testLogger())
	name := c.Name()
	if !strings.Contains(name, "t1") || !strings.Contains(name, "t2") {
		t.Errorf("Name() = %q, want it to contain target names", name)
	}
}

func TestRunSendsRequests(t *testing.T) {
	var hits int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.HTTPConfig{
		Enabled: true,
		Targets: []config.HTTPTarget{
			{
				Name:          "flood",
				URL:           srv.URL,
				Method:        "GET",
				Concurrency:   4,
				TotalRequests: 20,
			},
		},
	}
	c := httpflood.New(cfg, testLogger())

	if err := c.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt64(&hits); got != 20 {
		t.Errorf("server received %d requests, want 20", got)
	}
}

func TestRunRespectsContextCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	cfg := config.HTTPConfig{
		Enabled: true,
		Targets: []config.HTTPTarget{
			{
				Name:          "flood",
				URL:           srv.URL,
				Method:        "GET",
				Concurrency:   2,
				TotalRequests: 1000,
			},
		},
	}
	c := httpflood.New(cfg, testLogger())

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- c.Run(ctx) }()

	select {
	case <-done:
		// Run returned promptly after the context expired: good.
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestRollbackNoop(t *testing.T) {
	c := httpflood.New(config.HTTPConfig{}, testLogger())
	if err := c.Rollback(context.Background()); err != nil {
		t.Errorf("Rollback should be a no-op, got %v", err)
	}
}
