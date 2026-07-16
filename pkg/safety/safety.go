// Package safety provides guardrails for destructive chaos experiments.
package safety

import (
	"fmt"
	"os/exec"
	"regexp"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
)

// Policy is a compiled view of the configured safety guardrails.
type Policy struct {
	cfg                 config.SafetyConfig
	allowedNamePatterns []*regexp.Regexp
	blockedNamePatterns []*regexp.Regexp
}

// NewPolicy compiles the configured safety policy.
func NewPolicy(cfg config.SafetyConfig) (*Policy, error) {
	p := &Policy{cfg: cfg}

	for _, pattern := range cfg.AllowedNamePatterns {
		rx, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile allowed_name_patterns %q: %w", pattern, err)
		}
		p.allowedNamePatterns = append(p.allowedNamePatterns, rx)
	}
	for _, pattern := range cfg.BlockedNamePatterns {
		rx, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("compile blocked_name_patterns %q: %w", pattern, err)
		}
		p.blockedNamePatterns = append(p.blockedNamePatterns, rx)
	}

	return p, nil
}

// Preflight validates the overall run against the safety policy and logs a summary.
func Preflight(cfg *config.Config, approved bool, log *logger.Logger) (*Policy, error) {
	policy, err := NewPolicy(cfg.Safety)
	if err != nil {
		return nil, err
	}

	if !cfg.Safety.Enabled {
		log.Warn("Safety policy disabled")
		return policy, nil
	}

	destructiveActions := policy.EstimateDestructiveActions(cfg)
	if !cfg.DryRun && cfg.Safety.FailClosed && !cfg.Safety.AllowDestructiveActions && destructiveActions > 0 {
		return nil, fmt.Errorf("safety policy blocks destructive actions; set safety.allow_destructive_actions=true to proceed")
	}

	if !cfg.DryRun && cfg.Safety.RequireApproval && destructiveActions > 0 && !approved {
		return nil, fmt.Errorf("safety approval required; pass -approve or enable interactive confirmation")
	}

	if !cfg.DryRun {
		if err := policy.ValidateRun(cfg); err != nil {
			return nil, err
		}
	}

	log.Info("Safety summary: targets=%d destructive_actions=%d fail_closed=%t approval_required=%t allow_destructive=%t",
		policy.EstimateTargets(cfg),
		destructiveActions,
		cfg.Safety.FailClosed,
		cfg.Safety.RequireApproval,
		cfg.Safety.AllowDestructiveActions,
	)

	return policy, nil
}

// ValidateRun checks the configured namespaces and runtime requirements.
func (p *Policy) ValidateRun(cfg *config.Config) error {
	if cfg.Kubernetes.Enabled {
		if err := p.validateNamespaces(cfg.Kubernetes.Namespaces); err != nil {
			return err
		}
	}
	if err := p.validateActionLimits(cfg); err != nil {
		return err
	}
	if cfg.Network.Enabled {
		if err := p.validateBinaries(cfg); err != nil {
			return err
		}
	}
	return nil
}

// CheckK8sTarget validates a Kubernetes target resource before mutation.
func (p *Policy) CheckK8sTarget(namespace, name, action string) error {
	if !p.cfg.Enabled {
		return nil
	}
	if err := p.checkNamespace(namespace); err != nil {
		return err
	}
	if err := p.checkName(name); err != nil {
		return err
	}
	return p.checkAction(action, "kubernetes")
}

// CheckNetworkAction validates a network action before mutation.
func (p *Policy) CheckNetworkAction(action string) error {
	return p.checkAction(action, "network")
}

// CheckStressAction validates a stress action before mutation.
func (p *Policy) CheckStressAction(action string) error {
	return p.checkAction(action, "stress")
}

// DestructiveActionCount returns the number of configured destructive actions in a run.
func (p *Policy) DestructiveActionCount(cfg *config.Config) int {
	total := 0
	if cfg.Kubernetes.Enabled {
		for _, action := range cfg.Kubernetes.Actions {
			if isDestructiveK8sAction(action) {
				total += len(cfg.Kubernetes.Namespaces)
			}
		}
	}
	if cfg.Network.Enabled {
		total += len(cfg.Network.Actions)
	}
	if cfg.Stress.Enabled {
		total += len(cfg.Stress.Actions)
	}
	return total
}

// EstimateTargets returns a coarse upper bound on the number of targets touched in a run.
func (p *Policy) EstimateTargets(cfg *config.Config) int {
	total := 0
	if cfg.HTTP.Enabled {
		total += len(cfg.HTTP.Targets)
	}
	if cfg.GRPC.Enabled {
		total += len(cfg.GRPC.Targets)
	}
	if cfg.MQ.Enabled {
		total += len(cfg.MQ.Backends)
	}
	if cfg.Network.Enabled {
		total += len(cfg.Network.Actions)
	}
	if cfg.Stress.Enabled {
		total += len(cfg.Stress.Actions)
	}

	if cfg.Kubernetes.Enabled {
		k8sNamespaces := len(cfg.Kubernetes.Namespaces)
		if k8sNamespaces == 0 {
			k8sNamespaces = 1
		}
		k8sActions := len(cfg.Kubernetes.Actions)
		if k8sActions > 0 {
			total += k8sNamespaces * k8sActions
		}
	}
	return total
}

// EstimateDestructiveActions returns the number of configured destructive actions.
func (p *Policy) EstimateDestructiveActions(cfg *config.Config) int {
	return p.DestructiveActionCount(cfg)
}

func (p *Policy) validateNamespaces(namespaces []string) error {
	if len(p.cfg.AllowedNamespaces) > 0 {
		for _, ns := range namespaces {
			if !containsString(p.cfg.AllowedNamespaces, ns) {
				return fmt.Errorf("namespace %q is not in safety.allowed_namespaces", ns)
			}
		}
	}
	for _, ns := range namespaces {
		if containsString(p.cfg.BlockedNamespaces, ns) {
			return fmt.Errorf("namespace %q is blocked by safety.blocked_namespaces", ns)
		}
	}
	return nil
}

func (p *Policy) validateActionLimits(cfg *config.Config) error {
	if p.cfg.MaxTargetsPerRun > 0 && p.EstimateTargets(cfg) > p.cfg.MaxTargetsPerRun {
		return fmt.Errorf("estimated targets %d exceed safety.max_targets_per_run=%d", p.EstimateTargets(cfg), p.cfg.MaxTargetsPerRun)
	}
	if p.cfg.MaxDestructiveActionsPerRun > 0 && p.EstimateDestructiveActions(cfg) > p.cfg.MaxDestructiveActionsPerRun {
		return fmt.Errorf("estimated destructive actions %d exceed safety.max_destructive_actions_per_run=%d", p.EstimateDestructiveActions(cfg), p.cfg.MaxDestructiveActionsPerRun)
	}
	return nil
}

func (p *Policy) validateBinaries(cfg *config.Config) error {
	if len(cfg.Network.Actions) == 0 {
		return nil
	}
	if _, err := exec.LookPath("tc"); err != nil {
		return fmt.Errorf("network safety preflight: tc is not available in PATH: %w", err)
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		return fmt.Errorf("network safety preflight: iptables is not available in PATH: %w", err)
	}
	return nil
}

func (p *Policy) checkNamespace(namespace string) error {
	if len(p.cfg.AllowedNamespaces) > 0 && !containsString(p.cfg.AllowedNamespaces, namespace) {
		return fmt.Errorf("namespace %q is not allowed by safety.allowed_namespaces", namespace)
	}
	if containsString(p.cfg.BlockedNamespaces, namespace) {
		return fmt.Errorf("namespace %q is blocked by safety.blocked_namespaces", namespace)
	}
	return nil
}

func (p *Policy) checkName(name string) error {
	if name == "" {
		return nil
	}
	for _, rx := range p.blockedNamePatterns {
		if rx.MatchString(name) {
			return fmt.Errorf("resource %q is blocked by safety.blocked_name_patterns", name)
		}
	}
	if len(p.allowedNamePatterns) > 0 {
		for _, rx := range p.allowedNamePatterns {
			if rx.MatchString(name) {
				return nil
			}
		}
		return fmt.Errorf("resource %q does not match safety.allowed_name_patterns", name)
	}
	return nil
}

func (p *Policy) checkAction(action, module string) error {
	if !p.cfg.Enabled {
		return nil
	}
	if !p.cfg.AllowDestructiveActions && isDestructiveAction(module, action) {
		return fmt.Errorf("%s action %q is blocked until safety.allow_destructive_actions=true", module, action)
	}
	return nil
}

func isDestructiveAction(module, action string) bool {
	switch module {
	case "kubernetes":
		return isDestructiveK8sAction(action)
	case "network":
		return true
	case "stress":
		return true
	default:
		return false
	}
}

func isDestructiveK8sAction(action string) bool {
	switch action {
	case "kill_pod", "scale_down", "delete_service":
		return true
	default:
		return false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
