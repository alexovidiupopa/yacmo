package scheduler

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"yacmo/pkg/chaos"
	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// countingExperiment records how many times Run is invoked.
type countingExperiment struct {
	runs atomic.Int32
}

func (c *countingExperiment) Name() string { return "counting" }

func (c *countingExperiment) Run(context.Context) error {
	c.runs.Add(1)
	return nil
}

func (c *countingExperiment) Rollback(context.Context) error { return nil }

// newTestScheduler builds a Scheduler wired to an engine running exp.
func newTestScheduler(t *testing.T, cfg config.SchedulerConfig, exp chaos.Experiment) *Scheduler {
	t.Helper()
	log := logger.New("error")
	engineCfg := config.DefaultConfig()
	engineCfg.DryRun = false // exercise the real experiment path so runs are counted
	engine := chaos.NewEngine(engineCfg, log)
	engine.Register(exp)
	return New(cfg, engine, log, time.Millisecond)
}

func TestRunCronRequiresExpression(t *testing.T) {
	s := newTestScheduler(t, config.SchedulerConfig{Mode: "cron"}, &countingExperiment{})
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error for cron mode without expression, got nil")
	}
}

func TestRunCronRejectsInvalidExpression(t *testing.T) {
	s := newTestScheduler(t, config.SchedulerConfig{Mode: "cron", CronExpression: "not a cron"}, &countingExperiment{})
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error for invalid cron expression, got nil")
	}
}

func TestRunUnknownMode(t *testing.T) {
	s := newTestScheduler(t, config.SchedulerConfig{Mode: "bogus"}, &countingExperiment{})
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error for unknown mode, got nil")
	}
}

func TestRunCronStopsAtMaxExperiments(t *testing.T) {
	exp := &countingExperiment{}
	// "@every 1s" is the fastest schedule robfig honours (sub-second is
	// rounded up to one second), so one round takes ~1s.
	s := newTestScheduler(t, config.SchedulerConfig{
		Mode:           "cron",
		CronExpression: "@every 1s",
		MaxExperiments: 1,
	}, exp)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.Run(ctx); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
	if got := exp.runs.Load(); got != 1 {
		t.Fatalf("experiment ran %d times, want 1", got)
	}
}

func TestRunCronHonoursCancellation(t *testing.T) {
	exp := &countingExperiment{}
	s := newTestScheduler(t, config.SchedulerConfig{
		Mode:           "cron",
		CronExpression: "@every 1s",
	}, exp)

	// Cancel well before the first tick fires.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan error, 1)
	go func() { done <- s.Run(ctx) }()

	select {
	case err := <-done:
		if err != context.Canceled {
			t.Fatalf("Run error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
	if got := exp.runs.Load(); got != 0 {
		t.Fatalf("experiment ran %d times after cancel, want 0", got)
	}
}
