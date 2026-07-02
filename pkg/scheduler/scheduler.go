// Package scheduler provides scheduling logic for chaos experiments.
// It supports one-shot, continuous (interval-based), and cron-based scheduling.
package scheduler

import (
	"context"
	"fmt"
	"time"

	"yacmo/pkg/chaos"
	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// Scheduler runs the chaos engine on a schedule.
type Scheduler struct {
	cfg    config.SchedulerConfig
	engine *chaos.Engine
	log    *logger.Logger
	// Interval between rounds (used in continuous mode)
	interval time.Duration
}

// New creates a new Scheduler.
func New(cfg config.SchedulerConfig, engine *chaos.Engine, log *logger.Logger, interval time.Duration) *Scheduler {
	return &Scheduler{
		cfg:      cfg,
		engine:   engine,
		log:      log,
		interval: interval,
	}
}

// Run starts the scheduler and blocks until the context is cancelled or
// the maximum number of experiments is reached.
func (s *Scheduler) Run(ctx context.Context) error {
	switch s.cfg.Mode {
	case "once":
		return s.runOnce(ctx)
	case "continuous":
		return s.runContinuous(ctx)
	case "cron":
		return s.runCron(ctx)
	default:
		return fmt.Errorf("unknown scheduler mode: %s", s.cfg.Mode)
	}
}

// runOnce executes experiments a single time.
func (s *Scheduler) runOnce(ctx context.Context) error {
	s.log.Info("Scheduler mode: once")
	s.engine.RunAll(ctx)
	return nil
}

// runContinuous executes experiments on a fixed interval.
func (s *Scheduler) runContinuous(ctx context.Context) error {
	s.log.Info("Scheduler mode: continuous (interval=%s)", s.interval)

	round := 0
	for {
		round++
		s.log.Info("━━━ Round %d ━━━", round)
		s.engine.RunAll(ctx)

		if s.cfg.MaxExperiments > 0 && round >= s.cfg.MaxExperiments {
			s.log.Info("Reached max experiments (%d), stopping", s.cfg.MaxExperiments)
			return nil
		}

		select {
		case <-ctx.Done():
			s.log.Info("Context cancelled, stopping scheduler")
			return ctx.Err()
		case <-time.After(s.interval):
			// next round
		}
	}
}

// runCron executes experiments on a cron-like schedule.
// This is a simplified cron implementation that parses basic interval expressions.
// For a full cron implementation, consider using a library like robfig/cron.
func (s *Scheduler) runCron(ctx context.Context) error {
	if s.cfg.CronExpression == "" {
		return fmt.Errorf("cron mode requires a cron_expression")
	}

	// Parse a simple interval from the cron expression.
	// For a full implementation, integrate github.com/robfig/cron/v3.
	interval, err := parseSimpleCron(s.cfg.CronExpression)
	if err != nil {
		return fmt.Errorf("parsing cron expression: %w", err)
	}

	s.log.Info("Scheduler mode: cron (expression=%s, effective_interval=%s)", s.cfg.CronExpression, interval)

	round := 0
	for {
		// Wait until next tick
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}

		round++
		s.log.Info("━━━ Cron round %d ━━━", round)
		s.engine.RunAll(ctx)

		if s.cfg.MaxExperiments > 0 && round >= s.cfg.MaxExperiments {
			s.log.Info("Reached max experiments (%d), stopping", s.cfg.MaxExperiments)
			return nil
		}
	}
}

// parseSimpleCron parses a subset of cron expressions.
// Supported formats:
//   - "@every <duration>" e.g. "@every 5m", "@every 1h30m"
//   - "*/N * * * *" interpreted as every N minutes
func parseSimpleCron(expr string) (time.Duration, error) {
	// Handle @every syntax
	if len(expr) > 7 && expr[:7] == "@every " {
		d, err := time.ParseDuration(expr[7:])
		if err != nil {
			return 0, fmt.Errorf("invalid duration in @every: %w", err)
		}
		return d, nil
	}

	// Handle */N * * * * (every N minutes)
	var n int
	if _, err := fmt.Sscanf(expr, "*/%d * * * *", &n); err == nil && n > 0 {
		return time.Duration(n) * time.Minute, nil
	}

	return 0, fmt.Errorf("unsupported cron expression %q — use '@every <duration>' or '*/N * * * *'", expr)
}
