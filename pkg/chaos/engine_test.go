package chaos_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"yacmo/pkg/chaos"
	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

func testLogger() *logger.Logger { return logger.New("error") }

// fakeExperiment is a controllable chaos.Experiment for tests.
type fakeExperiment struct {
	name        string
	runErr      error
	runCalls    int32
	rollbackErr error
	rollbackNum int32
	order       *[]string
	orderMu     *sync.Mutex
}

func (f *fakeExperiment) Name() string { return f.name }

func (f *fakeExperiment) Run(ctx context.Context) error {
	atomic.AddInt32(&f.runCalls, 1)
	if f.order != nil {
		f.orderMu.Lock()
		*f.order = append(*f.order, "run:"+f.name)
		f.orderMu.Unlock()
	}
	return f.runErr
}

func (f *fakeExperiment) Rollback(ctx context.Context) error {
	atomic.AddInt32(&f.rollbackNum, 1)
	if f.order != nil {
		f.orderMu.Lock()
		*f.order = append(*f.order, "rollback:"+f.name)
		f.orderMu.Unlock()
	}
	return f.rollbackErr
}

// destructiveExperiment also reports a destructive action count.
type destructiveExperiment struct {
	fakeExperiment
	count int
}

func (d *destructiveExperiment) DestructiveActionCount() int { return d.count }

// flakyExperiment fails its first failFirst runs, then succeeds.
type flakyExperiment struct {
	name      string
	failFirst int
	calls     int32
}

func (f *flakyExperiment) Name() string { return f.name }
func (f *flakyExperiment) Run(ctx context.Context) error {
	n := atomic.AddInt32(&f.calls, 1)
	if int(n) <= f.failFirst {
		return errors.New("transient failure")
	}
	return nil
}
func (f *flakyExperiment) Rollback(ctx context.Context) error { return nil }

func newEngine(cfg *config.Config) *chaos.Engine {
	return chaos.NewEngine(cfg, testLogger())
}

func TestRegisterNamedAndGet(t *testing.T) {
	e := newEngine(config.DefaultConfig())
	exp := &fakeExperiment{name: "e1"}
	e.RegisterNamed("e1", exp)

	if got := e.GetExperiment("e1"); got != exp {
		t.Error("GetExperiment did not return the registered experiment")
	}
	if got := e.GetExperiment("missing"); got != nil {
		t.Error("GetExperiment for unknown id should return nil")
	}
}

func TestRunAllSequential(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false
	e := newEngine(cfg)

	a := &fakeExperiment{name: "a"}
	b := &fakeExperiment{name: "b"}
	e.Register(a)
	e.Register(b)

	var callbackCount int
	e.OnResult(func(r chaos.ExperimentResult) { callbackCount++ })

	results := e.RunAll(context.Background())

	if len(results) != 2 {
		t.Fatalf("got %d results, want 2", len(results))
	}
	if atomic.LoadInt32(&a.runCalls) != 1 || atomic.LoadInt32(&b.runCalls) != 1 {
		t.Error("each experiment should have run exactly once")
	}
	if callbackCount != 2 {
		t.Errorf("callback fired %d times, want 2", callbackCount)
	}
	for _, r := range results {
		if !r.Success {
			t.Errorf("experiment %s should have succeeded", r.ExperimentName)
		}
	}
}

func TestRunAllRecordsFailure(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false
	e := newEngine(cfg)

	e.Register(&fakeExperiment{name: "boom", runErr: errors.New("kaboom")})
	results := e.RunAll(context.Background())

	if len(results) != 1 || results[0].Success {
		t.Fatalf("expected 1 failed result, got %+v", results)
	}
	if results[0].Error == nil {
		t.Error("failed result should carry the error")
	}
}

func TestRunAllDryRunSkipsExecution(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = true
	e := newEngine(cfg)

	exp := &fakeExperiment{name: "a"}
	e.Register(exp)

	results := e.RunAll(context.Background())
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("dry-run should produce one successful (skipped) result, got %+v", results)
	}
	if atomic.LoadInt32(&exp.runCalls) != 0 {
		t.Error("Run must not be called in dry-run mode")
	}
	if results[0].Details != "dry-run: skipped" {
		t.Errorf("Details = %q, want %q", results[0].Details, "dry-run: skipped")
	}
}

func TestRollbackAllReverseOrder(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false
	e := newEngine(cfg)

	var order []string
	var mu sync.Mutex
	a := &fakeExperiment{name: "a", order: &order, orderMu: &mu}
	b := &fakeExperiment{name: "b", order: &order, orderMu: &mu}
	e.Register(a)
	e.Register(b)

	e.RollbackAll(context.Background())

	want := []string{"rollback:b", "rollback:a"}
	if len(order) != 2 || order[0] != want[0] || order[1] != want[1] {
		t.Errorf("rollback order = %v, want %v", order, want)
	}
}

func TestRunAllSafetyCapBlocks(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = true
	cfg.Safety.MaxDestructiveActionsPerRun = 2
	e := newEngine(cfg)

	// One experiment reporting 5 destructive actions exceeds the cap of 2.
	d := &destructiveExperiment{fakeExperiment: fakeExperiment{name: "big"}, count: 5}
	e.Register(d)

	results := e.RunAll(context.Background())
	if results != nil {
		t.Errorf("RunAll should return nil when the destructive cap is exceeded, got %+v", results)
	}
	if atomic.LoadInt32(&d.runCalls) != 0 {
		t.Error("experiment must not run when the safety cap is exceeded")
	}
}

func TestRunScenariosOrderingAndPrerequisites(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false

	var order []string
	var mu sync.Mutex

	cfg.Scenarios = []config.Scenario{
		{
			Name:    "second",
			Enabled: true,
			Order:   20,
			Steps:   []config.ScenarioStep{{Name: "b"}},
		},
		{
			Name:    "first",
			Enabled: true,
			Order:   10,
			Steps:   []config.ScenarioStep{{Name: "a"}},
		},
		{
			Name:          "needs-missing",
			Enabled:       true,
			Order:         30,
			Prerequisites: []string{"never-ran"},
			Steps:         []config.ScenarioStep{{Name: "c"}},
		},
	}

	e := newEngine(cfg)
	a := &fakeExperiment{name: "a", order: &order, orderMu: &mu}
	b := &fakeExperiment{name: "b", order: &order, orderMu: &mu}
	c := &fakeExperiment{name: "c", order: &order, orderMu: &mu}
	e.RegisterNamed("a", a)
	e.RegisterNamed("b", b)
	e.RegisterNamed("c", c)

	e.RunAll(context.Background())

	// "first" (order 10) runs before "second" (order 20).
	if len(order) != 2 || order[0] != "run:a" || order[1] != "run:b" {
		t.Errorf("scenario order = %v, want [run:a run:b]", order)
	}
	// "needs-missing" is skipped because its prerequisite never succeeded.
	if atomic.LoadInt32(&c.runCalls) != 0 {
		t.Error("scenario with unmet prerequisite must be skipped")
	}
}

func TestRunScenariosConditionalSteps(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false

	cfg.Scenarios = []config.Scenario{
		{
			Name:    "conditional",
			Enabled: true,
			Order:   10,
			Steps: []config.ScenarioStep{
				{Name: "fails"},
				{Name: "on_success_step", Condition: "on_success"},
				{Name: "on_failure_step", Condition: "on_failure"},
			},
		},
	}

	e := newEngine(cfg)
	fails := &fakeExperiment{name: "fails", runErr: errors.New("boom")}
	onSucc := &fakeExperiment{name: "on_success_step"}
	onFail := &fakeExperiment{name: "on_failure_step"}
	e.RegisterNamed("fails", fails)
	e.RegisterNamed("on_success_step", onSucc)
	e.RegisterNamed("on_failure_step", onFail)

	e.RunAll(context.Background())

	if atomic.LoadInt32(&onSucc.runCalls) != 0 {
		t.Error("on_success step must be skipped after a failure")
	}
	if atomic.LoadInt32(&onFail.runCalls) != 1 {
		t.Error("on_failure step must run after a failure")
	}
}

func TestRunScenarioStepRetries(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false

	cfg.Scenarios = []config.Scenario{
		{
			Name:    "retrying",
			Enabled: true,
			Order:   10,
			Steps:   []config.ScenarioStep{{Name: "flaky", Retries: 2}}, // 3 attempts
		},
	}

	e := newEngine(cfg)
	flaky := &flakyExperiment{name: "flaky", failFirst: 2} // succeeds on attempt 3
	e.RegisterNamed("flaky", flaky)

	results := e.RunAll(context.Background())

	if atomic.LoadInt32(&flaky.calls) != 3 {
		t.Errorf("flaky run called %d times, want 3", atomic.LoadInt32(&flaky.calls))
	}
	if len(results) != 1 || !results[0].Success {
		t.Errorf("step should ultimately succeed after retries, got %+v", results)
	}
}

func TestRunScenariosUnknownExperiment(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false
	cfg.Scenarios = []config.Scenario{
		{
			Name:    "bad",
			Enabled: true,
			Order:   10,
			Steps:   []config.ScenarioStep{{Name: "does-not-exist"}},
		},
	}

	e := newEngine(cfg)
	results := e.RunAll(context.Background())

	if len(results) != 1 || results[0].Success {
		t.Fatalf("unknown experiment should yield a failed result, got %+v", results)
	}
}

func TestRunScenariosParallel(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false
	cfg.Scenarios = []config.Scenario{
		{
			Name:     "parallel",
			Enabled:  true,
			Order:    10,
			Parallel: true,
			Steps: []config.ScenarioStep{
				{Name: "p1"},
				{Name: "p2"},
				{Name: "p3"},
			},
		},
	}

	e := newEngine(cfg)
	p1 := &fakeExperiment{name: "p1"}
	p2 := &fakeExperiment{name: "p2"}
	p3 := &fakeExperiment{name: "p3"}
	e.RegisterNamed("p1", p1)
	e.RegisterNamed("p2", p2)
	e.RegisterNamed("p3", p3)

	results := e.RunAll(context.Background())

	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}
	for _, p := range []*fakeExperiment{p1, p2, p3} {
		if atomic.LoadInt32(&p.runCalls) != 1 {
			t.Errorf("parallel step %s ran %d times, want 1", p.name, p.runCalls)
		}
	}
}

// TestRunScenariosParallelFailureGatesPrerequisite exercises the concurrent
// aggregation of a parallel scenario's success state: when any step fails, the
// scenario must be marked failed so that dependent scenarios are skipped. It
// also serves as a regression guard (run under -race) for the shared write to
// the per-scenario success flag across step goroutines.
func TestRunScenariosParallelFailureGatesPrerequisite(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Safety.Enabled = false
	cfg.Scenarios = []config.Scenario{
		{
			Name:     "chaos",
			Enabled:  true,
			Order:    10,
			Parallel: true,
			Steps: []config.ScenarioStep{
				{Name: "ok1"},
				{Name: "boom1"},
				{Name: "boom2"},
			},
		},
		{
			Name:          "recovery",
			Enabled:       true,
			Order:         20,
			Prerequisites: []string{"chaos"},
			Steps:         []config.ScenarioStep{{Name: "recover"}},
		},
	}

	e := newEngine(cfg)
	e.RegisterNamed("ok1", &fakeExperiment{name: "ok1"})
	// Two concurrently-failing steps both write the scenario success flag,
	// which is the exact interleaving that must be synchronized.
	e.RegisterNamed("boom1", &fakeExperiment{name: "boom1", runErr: errors.New("boom")})
	e.RegisterNamed("boom2", &fakeExperiment{name: "boom2", runErr: errors.New("boom")})
	recover := &fakeExperiment{name: "recover"}
	e.RegisterNamed("recover", recover)

	e.RunAll(context.Background())

	if atomic.LoadInt32(&recover.runCalls) != 0 {
		t.Error("recovery scenario must be skipped because a parallel step failed")
	}
}
