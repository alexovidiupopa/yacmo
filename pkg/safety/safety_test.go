package safety_test

import (
	"strings"
	"testing"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
	"yacmo/pkg/safety"
)

func testLogger() *logger.Logger {
	// "error" keeps test output quiet.
	return logger.New("error")
}

func baseSafety() config.SafetyConfig {
	return config.SafetyConfig{
		Enabled:                     true,
		FailClosed:                  true,
		RequireApproval:             true,
		AllowDestructiveActions:     false,
		BlockedNamespaces:           []string{"kube-system"},
		BlockedNamePatterns:         []string{`^kube-.*`},
		MaxTargetsPerRun:            20,
		MaxDestructiveActionsPerRun: 8,
	}
}

func TestNewPolicyInvalidPattern(t *testing.T) {
	cfg := baseSafety()
	cfg.AllowedNamePatterns = []string{"("}
	if _, err := safety.NewPolicy(cfg); err == nil {
		t.Error("NewPolicy should fail on an invalid regexp")
	}
}

func TestCheckK8sTarget(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(c *config.SafetyConfig)
		namespace string
		resource  string
		action    string
		wantErr   bool
	}{
		{
			name:      "allowed non-destructive-ish default blocks destructive",
			mutate:    func(c *config.SafetyConfig) {},
			namespace: "default",
			resource:  "web-abc",
			action:    "kill_pod",
			wantErr:   true, // destructive and AllowDestructiveActions=false
		},
		{
			name:      "destructive allowed when flag set",
			mutate:    func(c *config.SafetyConfig) { c.AllowDestructiveActions = true },
			namespace: "default",
			resource:  "web-abc",
			action:    "kill_pod",
			wantErr:   false,
		},
		{
			name:      "blocked namespace",
			mutate:    func(c *config.SafetyConfig) { c.AllowDestructiveActions = true },
			namespace: "kube-system",
			resource:  "web-abc",
			action:    "kill_pod",
			wantErr:   true,
		},
		{
			name:      "blocked name pattern",
			mutate:    func(c *config.SafetyConfig) { c.AllowDestructiveActions = true },
			namespace: "default",
			resource:  "kube-proxy",
			action:    "kill_pod",
			wantErr:   true,
		},
		{
			name: "not in allowed namespaces",
			mutate: func(c *config.SafetyConfig) {
				c.AllowDestructiveActions = true
				c.AllowedNamespaces = []string{"sandbox"}
			},
			namespace: "default",
			resource:  "web-abc",
			action:    "kill_pod",
			wantErr:   true,
		},
		{
			name: "does not match allowed name patterns",
			mutate: func(c *config.SafetyConfig) {
				c.AllowDestructiveActions = true
				c.AllowedNamePatterns = []string{`^web-.*`}
			},
			namespace: "default",
			resource:  "db-1",
			action:    "kill_pod",
			wantErr:   true,
		},
		{
			name: "matches allowed name pattern",
			mutate: func(c *config.SafetyConfig) {
				c.AllowDestructiveActions = true
				c.AllowedNamePatterns = []string{`^web-.*`}
			},
			namespace: "default",
			resource:  "web-1",
			action:    "kill_pod",
			wantErr:   false,
		},
		{
			name:      "policy disabled allows anything",
			mutate:    func(c *config.SafetyConfig) { c.Enabled = false },
			namespace: "kube-system",
			resource:  "kube-proxy",
			action:    "kill_pod",
			wantErr:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := baseSafety()
			tt.mutate(&cfg)
			p, err := safety.NewPolicy(cfg)
			if err != nil {
				t.Fatalf("NewPolicy: %v", err)
			}
			err = p.CheckK8sTarget(tt.namespace, tt.resource, tt.action)
			if (err != nil) != tt.wantErr {
				t.Errorf("CheckK8sTarget() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestCheckNetworkAndStressActions(t *testing.T) {
	cfg := baseSafety() // AllowDestructiveActions=false
	p, _ := safety.NewPolicy(cfg)
	if err := p.CheckNetworkAction("latency"); err == nil {
		t.Error("network action should be blocked when destructive actions are disallowed")
	}
	if err := p.CheckStressAction("cpu"); err == nil {
		t.Error("stress action should be blocked when destructive actions are disallowed")
	}

	cfg.AllowDestructiveActions = true
	p, _ = safety.NewPolicy(cfg)
	if err := p.CheckNetworkAction("latency"); err != nil {
		t.Errorf("network action should be allowed when destructive actions permitted: %v", err)
	}
	if err := p.CheckStressAction("cpu"); err != nil {
		t.Errorf("stress action should be allowed when destructive actions permitted: %v", err)
	}
}

func TestDestructiveActionCountAndEstimateTargets(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Kubernetes.Enabled = true
	cfg.Kubernetes.Namespaces = []string{"a", "b"}
	cfg.Kubernetes.Actions = []string{"kill_pod", "scale_down"} // both destructive
	cfg.Stress.Enabled = true
	cfg.Stress.Actions = []string{"cpu", "memory"}
	cfg.HTTP.Enabled = true
	cfg.HTTP.Targets = []config.HTTPTarget{{Name: "t", URL: "http://x"}}

	p, err := safety.NewPolicy(cfg.Safety)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}

	// k8s: 2 actions * 2 namespaces = 4; stress: 2 => 6 destructive.
	if got := p.DestructiveActionCount(cfg); got != 6 {
		t.Errorf("DestructiveActionCount() = %d, want 6", got)
	}

	// targets: http(1) + stress(2) + k8s(2 ns * 2 actions = 4) = 7.
	if got := p.EstimateTargets(cfg); got != 7 {
		t.Errorf("EstimateTargets() = %d, want 7", got)
	}
}

func TestPreflightDryRunBypassesGuards(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = true
	cfg.Stress.Enabled = true
	cfg.Stress.Actions = []string{"cpu"}
	// FailClosed true and AllowDestructiveActions false would normally block,
	// but dry-run should bypass.
	if _, err := safety.Preflight(cfg, false, testLogger()); err != nil {
		t.Errorf("Preflight in dry-run should succeed, got: %v", err)
	}
}

func TestPreflightFailClosedBlocksDestructive(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Stress.Enabled = true
	cfg.Stress.Actions = []string{"cpu"}
	cfg.Safety.AllowDestructiveActions = false
	cfg.Safety.FailClosed = true

	_, err := safety.Preflight(cfg, true, testLogger())
	if err == nil {
		t.Fatal("Preflight should block destructive actions when fail-closed and not allowed")
	}
	if !strings.Contains(err.Error(), "allow_destructive_actions") {
		t.Errorf("error should mention allow_destructive_actions, got: %v", err)
	}
}

func TestPreflightRequiresApproval(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.DryRun = false
	cfg.Stress.Enabled = true
	cfg.Stress.Actions = []string{"cpu"}
	cfg.Safety.AllowDestructiveActions = true // pass fail-closed gate
	cfg.Safety.FailClosed = true
	cfg.Safety.RequireApproval = true

	if _, err := safety.Preflight(cfg, false, testLogger()); err == nil {
		t.Error("Preflight should require approval when not approved")
	}
	if _, err := safety.Preflight(cfg, true, testLogger()); err != nil {
		t.Errorf("Preflight should succeed once approved, got: %v", err)
	}
}

func TestValidateRunActionLimits(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Stress.Enabled = true
	cfg.Stress.Actions = []string{"cpu", "memory", "disk_io"}
	cfg.Safety.MaxTargetsPerRun = 2 // 3 stress actions exceed this

	p, err := safety.NewPolicy(cfg.Safety)
	if err != nil {
		t.Fatalf("NewPolicy: %v", err)
	}
	if err := p.ValidateRun(cfg); err == nil {
		t.Error("ValidateRun should fail when estimated targets exceed max_targets_per_run")
	}

	cfg.Safety.MaxTargetsPerRun = 20
	p, _ = safety.NewPolicy(cfg.Safety)
	if err := p.ValidateRun(cfg); err != nil {
		t.Errorf("ValidateRun should pass within limits, got: %v", err)
	}
}
