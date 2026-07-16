// Package chaos defines the core engine that orchestrates chaos experiments.
package chaos

import (
	"context"
	"fmt"
	"sync"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// Experiment represents a single chaos experiment that can be executed.
type Experiment interface {
	// Name returns a human-readable name for the experiment.
	Name() string
	// Run executes the chaos experiment. It should respect context cancellation.
	Run(ctx context.Context) error
	// Rollback undoes the chaos experiment (best-effort).
	Rollback(ctx context.Context) error
}

// ExperimentResult holds the outcome of a single experiment execution.
type ExperimentResult struct {
	ExperimentName string
	Success        bool
	Error          error
	Details        string
	StartedAt      time.Time
	Duration       time.Duration
}

// ResultCallback is called after each experiment completes.
// It allows external systems (metrics, reports, notifications) to react.
type ResultCallback func(result ExperimentResult)

// Engine orchestrates chaos experiments.
type Engine struct {
	cfg         *config.Config
	log         *logger.Logger
	experiments []Experiment
	results     []ExperimentResult
	callbacks   []ResultCallback
	mu          sync.Mutex
}

// NewEngine creates a new chaos engine.
func NewEngine(cfg *config.Config, log *logger.Logger) *Engine {
	return &Engine{
		cfg: cfg,
		log: log,
	}
}

// OnResult registers a callback that fires after each experiment.
func (e *Engine) OnResult(cb ResultCallback) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.callbacks = append(e.callbacks, cb)
}

// Register adds an experiment to the engine.
func (e *Engine) Register(exp Experiment) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.experiments = append(e.experiments, exp)
	e.log.Info("Registered experiment: %s", exp.Name())
}

// RunAll executes all registered experiments sequentially.
func (e *Engine) RunAll(ctx context.Context) []ExperimentResult {
	e.mu.Lock()
	experiments := make([]Experiment, len(e.experiments))
	copy(experiments, e.experiments)
	callbacks := make([]ResultCallback, len(e.callbacks))
	copy(callbacks, e.callbacks)
	e.mu.Unlock()

	if e.cfg.Safety.Enabled && !e.cfg.DryRun && e.cfg.Safety.MaxDestructiveActionsPerRun > 0 {
		if destructive := destructiveActionCount(experiments); destructive > e.cfg.Safety.MaxDestructiveActionsPerRun {
			e.log.Error("Safety cap exceeded: %d destructive actions configured (max %d)", destructive, e.cfg.Safety.MaxDestructiveActionsPerRun)
			return nil
		}
	}

	var results []ExperimentResult

	for _, exp := range experiments {
		select {
		case <-ctx.Done():
			e.log.Warn("Context cancelled, stopping experiments")
			return results
		default:
		}

		e.log.Info("▶ Starting experiment: %s", exp.Name())
		startedAt := time.Now()

		if e.cfg.DryRun {
			e.log.Info("  [DRY-RUN] Skipping actual execution of: %s", exp.Name())
			result := ExperimentResult{
				ExperimentName: exp.Name(),
				Success:        true,
				Details:        "dry-run: skipped",
				StartedAt:      startedAt,
				Duration:       time.Since(startedAt),
			}
			results = append(results, result)
			for _, cb := range callbacks {
				cb(result)
			}
			continue
		}

		err := exp.Run(ctx)
		duration := time.Since(startedAt)
		result := ExperimentResult{
			ExperimentName: exp.Name(),
			Success:        err == nil,
			Error:          err,
			StartedAt:      startedAt,
			Duration:       duration,
		}

		if err != nil {
			e.log.Error("  ✗ Experiment %s failed after %s: %v", exp.Name(), duration, err)
			result.Details = fmt.Sprintf("error: %v", err)
		} else {
			e.log.Info("  ✓ Experiment %s completed in %s", exp.Name(), duration)
			result.Details = "success"
		}

		results = append(results, result)
		for _, cb := range callbacks {
			cb(result)
		}
	}

	e.mu.Lock()
	e.results = append(e.results, results...)
	e.mu.Unlock()

	return results
}

// RollbackAll rolls back all registered experiments (in reverse order).
func (e *Engine) RollbackAll(ctx context.Context) {
	e.mu.Lock()
	experiments := make([]Experiment, len(e.experiments))
	copy(experiments, e.experiments)
	e.mu.Unlock()

	e.log.Info("Rolling back %d experiments...", len(experiments))
	for i := len(experiments) - 1; i >= 0; i-- {
		exp := experiments[i]
		e.log.Info("↺ Rolling back: %s", exp.Name())
		if err := exp.Rollback(ctx); err != nil {
			e.log.Error("  Rollback failed for %s: %v", exp.Name(), err)
		} else {
			e.log.Info("  Rollback completed for %s", exp.Name())
		}
	}
}

// Results returns all collected experiment results.
func (e *Engine) Results() []ExperimentResult {
	e.mu.Lock()
	defer e.mu.Unlock()
	out := make([]ExperimentResult, len(e.results))
	copy(out, e.results)
	return out
}

// PrintSummary prints a summary of all results.
func (e *Engine) PrintSummary() {
	results := e.Results()
	if len(results) == 0 {
		e.log.Info("No experiments were executed.")
		return
	}

	e.log.Info("═══════════════════════════════════════")
	e.log.Info("  CHAOS EXPERIMENT SUMMARY")
	e.log.Info("═══════════════════════════════════════")

	succeeded := 0
	failed := 0
	for _, r := range results {
		status := "✓"
		if !r.Success {
			status = "✗"
			failed++
		} else {
			succeeded++
		}
		e.log.Info("  %s %s — %s (%s)", status, r.ExperimentName, r.Details, r.Duration)
	}

	e.log.Info("───────────────────────────────────────")
	e.log.Info("  Total: %d | Passed: %d | Failed: %d", len(results), succeeded, failed)
	e.log.Info("═══════════════════════════════════════")
}

type destructiveReporter interface {
	DestructiveActionCount() int
}

func destructiveActionCount(experiments []Experiment) int {
	total := 0
	for _, exp := range experiments {
		if reporter, ok := exp.(destructiveReporter); ok {
			total += reporter.DestructiveActionCount()
		}
	}
	return total
}
