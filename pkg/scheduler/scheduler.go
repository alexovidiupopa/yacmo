// Package scheduler provides scheduling logic for chaos experiments.
// It supports one-shot, continuous (interval-based), and cron-based scheduling.
package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"

	"yacmo/pkg/chaos"
	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// cronParser accepts standard 5-field cron expressions ("*/5 * * * *"),
// the "@every <duration>" form, and the named descriptors ("@hourly",
// "@daily", "@weekly", "@monthly", "@yearly").
var cronParser = cron.NewParser(
	cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor,
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

// runCron executes experiments on a cron schedule. It supports standard
// 5-field cron expressions, "@every <duration>", and named descriptors
// (see cronParser), computing the next fire time before each round.
func (s *Scheduler) runCron(ctx context.Context) error {
	if s.cfg.CronExpression == "" {
		return fmt.Errorf("cron mode requires a cron_expression")
	}

	schedule, err := cronParser.Parse(s.cfg.CronExpression)
	if err != nil {
		return fmt.Errorf("parsing cron expression %q: %w", s.cfg.CronExpression, err)
	}

	s.log.Info("Scheduler mode: cron (expression=%s)", s.cfg.CronExpression)

	round := 0
	for {
		now := time.Now()
		next := schedule.Next(now)
		wait := next.Sub(now)
		s.log.Debug("Next cron round at %s (in %s)", next.Format(time.RFC3339), wait.Round(time.Second))

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
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
