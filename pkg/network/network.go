// Package network provides network-level chaos experiments.
// It simulates DNS failures, adds latency, drops packets, and corrupts traffic
// using Linux tc (traffic control) and iptables commands.
package network

import (
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"yacmo/pkg/config"
	"yacmo/pkg/logger"
	"yacmo/pkg/safety"
)

// ChaosNetwork implements chaos.Experiment for network-level chaos.
type ChaosNetwork struct {
	cfg     config.NetworkConfig
	log     *logger.Logger
	policy  *safety.Policy
	applied []rollbackCmd // commands to undo on rollback
}

type rollbackCmd struct {
	description string
	cmd         string
	args        []string
}

// New creates a new network chaos experiment.
func New(cfg config.NetworkConfig, policy *safety.Policy, log *logger.Logger) *ChaosNetwork {
	return &ChaosNetwork{
		cfg:    cfg,
		log:    log,
		policy: policy,
	}
}

// Name returns the experiment name.
func (c *ChaosNetwork) Name() string {
	return fmt.Sprintf("network-chaos[iface=%s, actions=%s]",
		c.cfg.Interface, strings.Join(c.cfg.Actions, ","))
}

// Run executes the network chaos experiment.
func (c *ChaosNetwork) Run(ctx context.Context) error {
	if runtime.GOOS != "linux" {
		c.log.Warn("Network chaos uses Linux tc/iptables — limited support on %s", runtime.GOOS)
	}

	for _, action := range c.cfg.Actions {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		switch action {
		case "latency":
			if err := c.checkAction("latency"); err != nil {
				return err
			}
			if err := c.injectLatency(ctx); err != nil {
				return fmt.Errorf("latency injection: %w", err)
			}
		case "packet_loss":
			if err := c.checkAction("packet_loss"); err != nil {
				return err
			}
			if err := c.injectPacketLoss(ctx); err != nil {
				return fmt.Errorf("packet loss: %w", err)
			}
		case "dns_failure":
			if err := c.checkAction("dns_failure"); err != nil {
				return err
			}
			if err := c.injectDNSFailure(ctx); err != nil {
				return fmt.Errorf("dns failure: %w", err)
			}
		case "bandwidth_limit":
			if err := c.checkAction("bandwidth_limit"); err != nil {
				return err
			}
			if err := c.injectBandwidthLimit(ctx); err != nil {
				return fmt.Errorf("bandwidth limit: %w", err)
			}
		case "corrupt":
			if err := c.checkAction("corrupt"); err != nil {
				return err
			}
			if err := c.injectCorruption(ctx); err != nil {
				return fmt.Errorf("corruption: %w", err)
			}
		default:
			c.log.Warn("Unknown network action: %s", action)
		}
	}

	// If a duration is set, wait and then auto-rollback
	if c.cfg.Duration > 0 {
		c.log.Info("⏱️  Network chaos active for %s", c.cfg.Duration)
		select {
		case <-time.After(c.cfg.Duration):
			c.log.Info("Duration elapsed, rolling back network chaos")
			return c.Rollback(ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return nil
}

// DestructiveActionCount returns the number of destructive network actions configured.
func (c *ChaosNetwork) DestructiveActionCount() int {
	return len(c.cfg.Actions)
}

// Rollback undoes all applied network chaos rules.
func (c *ChaosNetwork) Rollback(ctx context.Context) error {
	var errs []string
	// Rollback in reverse order
	for i := len(c.applied) - 1; i >= 0; i-- {
		r := c.applied[i]
		c.log.Info("↺ Rollback: %s", r.description)
		cmd := exec.CommandContext(ctx, r.cmd, r.args...)
		if out, err := cmd.CombinedOutput(); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v (output: %s)", r.description, err, string(out)))
		}
	}
	c.applied = nil
	if len(errs) > 0 {
		return fmt.Errorf("rollback errors: %s", strings.Join(errs, "; "))
	}
	return nil
}

// injectLatency adds artificial latency using tc netem.
func (c *ChaosNetwork) injectLatency(ctx context.Context) error {
	iface := c.cfg.Interface
	latencyMs := c.cfg.LatencyMs
	jitterMs := c.cfg.JitterMs
	if latencyMs <= 0 {
		latencyMs = 100
	}

	args := []string{"qdisc", "add", "dev", iface, "root", "netem",
		"delay", fmt.Sprintf("%dms", latencyMs)}
	if jitterMs > 0 {
		args = append(args, fmt.Sprintf("%dms", jitterMs))
	}

	c.log.Info("🐌 Injecting %dms latency (±%dms jitter) on %s", latencyMs, jitterMs, iface)
	if err := c.runCmd(ctx, "tc", args...); err != nil {
		return err
	}

	c.applied = append(c.applied, rollbackCmd{
		description: fmt.Sprintf("remove latency from %s", iface),
		cmd:         "tc",
		args:        []string{"qdisc", "del", "dev", iface, "root"},
	})
	return nil
}

// injectPacketLoss drops a percentage of packets using tc netem.
func (c *ChaosNetwork) injectPacketLoss(ctx context.Context) error {
	iface := c.cfg.Interface
	lossPct := c.cfg.PacketLossPercent
	if lossPct <= 0 {
		lossPct = 5.0
	}

	c.log.Info("📦 Injecting %.1f%% packet loss on %s", lossPct, iface)
	args := []string{"qdisc", "add", "dev", iface, "root", "netem",
		"loss", fmt.Sprintf("%.2f%%", lossPct)}

	if err := c.runCmd(ctx, "tc", args...); err != nil {
		return err
	}

	c.applied = append(c.applied, rollbackCmd{
		description: fmt.Sprintf("remove packet loss from %s", iface),
		cmd:         "tc",
		args:        []string{"qdisc", "del", "dev", iface, "root"},
	})
	return nil
}

// injectDNSFailure blocks DNS traffic using iptables.
func (c *ChaosNetwork) injectDNSFailure(ctx context.Context) error {
	c.log.Info("🔇 Blocking DNS (port 53) via iptables")

	// Block UDP DNS
	if err := c.runCmd(ctx, "iptables",
		"-A", "OUTPUT", "-p", "udp", "--dport", "53", "-j", "DROP"); err != nil {
		return err
	}
	c.applied = append(c.applied, rollbackCmd{
		description: "unblock UDP DNS",
		cmd:         "iptables",
		args:        []string{"-D", "OUTPUT", "-p", "udp", "--dport", "53", "-j", "DROP"},
	})

	// Block TCP DNS
	if err := c.runCmd(ctx, "iptables",
		"-A", "OUTPUT", "-p", "tcp", "--dport", "53", "-j", "DROP"); err != nil {
		return err
	}
	c.applied = append(c.applied, rollbackCmd{
		description: "unblock TCP DNS",
		cmd:         "iptables",
		args:        []string{"-D", "OUTPUT", "-p", "tcp", "--dport", "53", "-j", "DROP"},
	})

	return nil
}

// injectBandwidthLimit throttles bandwidth using tc tbf.
func (c *ChaosNetwork) injectBandwidthLimit(ctx context.Context) error {
	iface := c.cfg.Interface
	rateKbps := c.cfg.BandwidthLimitKbps
	if rateKbps <= 0 {
		rateKbps = 256
	}

	c.log.Info("🚦 Limiting bandwidth to %d kbps on %s", rateKbps, iface)
	args := []string{"qdisc", "add", "dev", iface, "root", "tbf",
		"rate", fmt.Sprintf("%dkbit", rateKbps),
		"burst", "32kbit",
		"latency", "400ms"}

	if err := c.runCmd(ctx, "tc", args...); err != nil {
		return err
	}

	c.applied = append(c.applied, rollbackCmd{
		description: fmt.Sprintf("remove bandwidth limit from %s", iface),
		cmd:         "tc",
		args:        []string{"qdisc", "del", "dev", iface, "root"},
	})
	return nil
}

// injectCorruption corrupts a percentage of packets using tc netem.
func (c *ChaosNetwork) injectCorruption(ctx context.Context) error {
	iface := c.cfg.Interface
	corruptPct := c.cfg.CorruptPercent
	if corruptPct <= 0 {
		corruptPct = 5.0
	}

	c.log.Info("💥 Injecting %.1f%% packet corruption on %s", corruptPct, iface)
	args := []string{"qdisc", "add", "dev", iface, "root", "netem",
		"corrupt", fmt.Sprintf("%.2f%%", corruptPct)}

	if err := c.runCmd(ctx, "tc", args...); err != nil {
		return err
	}

	c.applied = append(c.applied, rollbackCmd{
		description: fmt.Sprintf("remove packet corruption from %s", iface),
		cmd:         "tc",
		args:        []string{"qdisc", "del", "dev", iface, "root"},
	})
	return nil
}

func (c *ChaosNetwork) checkAction(action string) error {
	if c.policy == nil {
		return nil
	}
	return c.policy.CheckNetworkAction(action)
}

// runCmd executes a system command.
func (c *ChaosNetwork) runCmd(ctx context.Context, name string, args ...string) error {
	c.log.Debug("exec: %s %s", name, strings.Join(args, " "))
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %w (output: %s)", name, strings.Join(args, " "), err, string(out))
	}
	return nil
}
