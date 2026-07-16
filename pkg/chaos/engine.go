// Package chaos defines the core engine that orchestrates chaos experiments.
package chaos

import (
	"context"
	"fmt"
	"sort"
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
	// namedExperiments holds experiments registered with a stable name
	namedExperiments map[string]Experiment
	results          []ExperimentResult
	callbacks        []ResultCallback
	mu               sync.Mutex
}

// NewEngine creates a new chaos engine.
func NewEngine(cfg *config.Config, log *logger.Logger) *Engine {
	return &Engine{
		cfg:              cfg,
		log:              log,
		namedExperiments: make(map[string]Experiment),
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

// RegisterNamed registers an experiment by a stable identifier so scenarios can reference it.
func (e *Engine) RegisterNamed(id string, exp Experiment) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.namedExperiments[id] = exp
	e.experiments = append(e.experiments, exp)
	e.log.Info("Registered experiment: %s (id=%s)", exp.Name(), id)
}

// GetExperiment returns a named experiment if registered, otherwise nil.
func (e *Engine) GetExperiment(id string) Experiment {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.namedExperiments[id]
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

	// If scenarios are defined in the config, execute them instead of the simple sequential list.
	if e.cfg != nil && len(e.cfg.Scenarios) > 0 {
		results = e.runScenarios(ctx, callbacks)
		e.mu.Lock()
		e.results = append(e.results, results...)
		e.mu.Unlock()
		return results
	}

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

// runScenarios executes configured scenarios (ordering, parallel groups, prerequisites, retries, and conditional steps)
func (e *Engine) runScenarios(ctx context.Context, callbacks []ResultCallback) []ExperimentResult {
	// Copy scenarios and sort by Order
	scenarios := make([]config.Scenario, len(e.cfg.Scenarios))
	copy(scenarios, e.cfg.Scenarios)
	sort.Slice(scenarios, func(i, j int) bool {
		return scenarios[i].Order < scenarios[j].Order
	})

	var resultsMu sync.Mutex
	var results []ExperimentResult

	// track scenario success for prerequisites
	scenarioSuccess := make(map[string]bool)

	for _, sc := range scenarios {
		if !sc.Enabled {
			e.log.Info("Skipping disabled scenario: %s", sc.Name)
			continue
		}

		// check prerequisites
		skip := false
		for _, pre := range sc.Prerequisites {
			if ok := scenarioSuccess[pre]; !ok {
				e.log.Warn("Skipping scenario %s because prerequisite %s did not succeed", sc.Name, pre)
				skip = true
				break
			}
		}
		if skip {
			scenarioSuccess[sc.Name] = false
			continue
		}

		e.log.Info("▶ Starting scenario: %s", sc.Name)

		// run steps
		allSucceeded := true
		if sc.Parallel {
			var wg sync.WaitGroup
			for _, step := range sc.Steps {
				wg.Add(1)
				go func(st config.ScenarioStep) {
					defer wg.Done()
					r := e.runScenarioStep(ctx, sc.Name, st, sc.Retries, callbacks)
					resultsMu.Lock()
					results = append(results, r)
					resultsMu.Unlock()
					if !r.Success {
						allSucceeded = false
					}
				}(step)
			}
			wg.Wait()
		} else {
			prevSuccess := true
			for _, step := range sc.Steps {
				// condition: "always" (default), "on_success", "on_failure"
				cond := step.Condition
				if cond == "" {
					cond = "always"
				}
				if cond == "on_success" && !prevSuccess {
					e.log.Info("  Skipping step %s due to condition on_success and previous failure", step.Name)
					continue
				}
				if cond == "on_failure" && prevSuccess {
					e.log.Info("  Skipping step %s due to condition on_failure and previous success", step.Name)
					continue
				}
				r := e.runScenarioStep(ctx, sc.Name, step, sc.Retries, callbacks)
				resultsMu.Lock()
				results = append(results, r)
				resultsMu.Unlock()
				if !r.Success {
					prevSuccess = false
					allSucceeded = false
				} else {
					prevSuccess = true
				}
			}
		}

		scenarioSuccess[sc.Name] = allSucceeded
	}

	return results
}

// runScenarioStep executes a single scenario step with retry logic.
func (e *Engine) runScenarioStep(ctx context.Context, scenarioName string, step config.ScenarioStep, scenarioRetries int, callbacks []ResultCallback) ExperimentResult {
	exp := e.GetExperiment(step.Name)
	startedAt := time.Now()

	if exp == nil {
		e.log.Error("  ✗ Unknown experiment id referenced in scenario %s: %s", scenarioName, step.Name)
		res := ExperimentResult{
			ExperimentName: step.Name,
			Success:        false,
			Error:          fmt.Errorf("unknown experiment %s", step.Name),
			StartedAt:      startedAt,
			Duration:       time.Since(startedAt),
		}
		return res
	}

	// Determine number of attempts (retries + 1)
	attempts := 1
	if step.Retries > 0 {
		attempts = step.Retries + 1
	} else if scenarioRetries > 0 {
		attempts = scenarioRetries + 1
	}

	var result ExperimentResult
	for attempt := 1; attempt <= attempts; attempt++ {
		select {
		case <-ctx.Done():
			e.log.Warn("Context cancelled while running step %s", step.Name)
			result = ExperimentResult{
				ExperimentName: step.Name,
				Success:        false,
				Error:          ctx.Err(),
				StartedAt:      startedAt,
				Duration:       time.Since(startedAt),
			}
			return result
		default:
		}

		if e.cfg.DryRun {
			e.log.Info("  [DRY-RUN] Skipping actual execution of: %s", exp.Name())
			result = ExperimentResult{
				ExperimentName: exp.Name(),
				Success:        true,
				Details:        "dry-run: skipped",
				StartedAt:      startedAt,
				Duration:       time.Since(startedAt),
			}
			break
		}

		err := exp.Run(ctx)
		result = ExperimentResult{
			ExperimentName: exp.Name(),
			Success:        err == nil,
			Error:          err,
			StartedAt:      startedAt,
			Duration:       time.Since(startedAt),
		}

		if err == nil {
			e.log.Info("  ✓ Step %s completed in %s", step.Name, result.Duration)
			result.Details = "success"
			break
		}

		result.Details = fmt.Sprintf("error: %v", err)
		e.log.Error("  ✗ Step %s failed on attempt %d/%d: %v", step.Name, attempt, attempts, err)
		if attempt < attempts {
			e.log.Info("  ↻ Retrying step %s", step.Name)
		}
	}

	// invoke callbacks
	for _, cb := range callbacks {
		cb(result)
	}

	return result
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
