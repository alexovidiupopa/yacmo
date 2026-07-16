// YACMO — Yet Another Chaos Monkey
//
// A chaos engineering tool for Kubernetes environments that supports:
//   - Killing/scaling Kubernetes pods, deployments, and services
//   - HTTP traffic injection (flood, latency injection, payload mutation)
//   - gRPC traffic injection
//   - Message queue traffic injection (RabbitMQ, Kafka, NATS)
//   - Network chaos (latency, packet loss, DNS failure, bandwidth limiting)
//   - Resource stress (CPU burn, memory pressure, disk I/O, disk fill)
//   - Pre/post health checks to measure blast radius
//   - Prometheus metrics exposition
//   - Structured JSON reports
//   - Webhook notifications (Slack, Discord, generic)
//
// Usage:
//
//	yacmo -config config.json [-dry-run] [-log-level debug|info|warn|error] [-approve]
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"yacmo/pkg/chaos"
	"yacmo/pkg/config"
	"yacmo/pkg/grpcflood"
	"yacmo/pkg/healthcheck"
	"yacmo/pkg/httpflood"
	"yacmo/pkg/k8s"
	"yacmo/pkg/logger"
	"yacmo/pkg/metrics"
	"yacmo/pkg/mqflood"
	"yacmo/pkg/network"
	"yacmo/pkg/notify"
	"yacmo/pkg/report"
	"yacmo/pkg/safety"
	"yacmo/pkg/scheduler"
	"yacmo/pkg/stress"
)

const banner = `
██╗   ██╗ █████╗  ██████╗███╗   ███╗ ██████╗
╚██╗ ██╔╝██╔══██╗██╔════╝████╗ ████║██╔═══██╗
 ╚████╔╝ ███████║██║     ██╔████╔██║██║   ██║
  ╚██╔╝  ██╔══██║██║     ██║╚██╔╝██║██║   ██║
   ██║   ██║  ██║╚██████╗██║ ╚═╝ ██║╚██████╔╝
   ╚═╝   ╚═╝  ╚═╝ ╚═════╝╚═╝     ╚═╝ ╚═════╝
  Yet Another Chaos Monkey — Chaos Engineering Tool
`

func main() {
	// CLI flags
	configPath := flag.String("config", "config.json", "Path to configuration file")
	dryRun := flag.Bool("dry-run", false, "Enable dry-run mode (no actual changes)")
	logLevel := flag.String("log-level", "", "Log level override (debug, info, warn, error)")
	approve := flag.Bool("approve", false, "Approve destructive actions after safety preflight")
	showVersion := flag.Bool("version", false, "Show version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("YACMO v0.2.0 — Yet Another Chaos Monkey")
		os.Exit(0)
	}

	fmt.Print(banner)

	// Load configuration
	cfg, err := config.LoadFromFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading config: %v\n", err)
		fmt.Fprintf(os.Stderr, "Hint: create a config file or use -config <path>\n")
		os.Exit(1)
	}

	// Apply CLI overrides
	if *dryRun {
		cfg.DryRun = true
	}
	if *logLevel != "" {
		cfg.LogLevel = *logLevel
	}

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "Invalid configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log := logger.New(cfg.LogLevel)

	if cfg.DryRun {
		log.Warn("DRY-RUN MODE: no actual changes will be made")
	}

	policyPreview, err := safety.NewPolicy(cfg.Safety)
	if err != nil {
		log.Error("Safety preflight failed: %v", err)
		os.Exit(1)
	}

	approved := *approve
	if !approved && cfg.Safety.Enabled && cfg.Safety.InteractiveConfirm && policyPreview.EstimateDestructiveActions(cfg) > 0 {
		approved = promptForApproval(os.Stdin, log)
	}

	policy, err := safety.Preflight(cfg, approved, log)
	if err != nil {
		log.Error("Safety preflight failed: %v", err)
		os.Exit(1)
	}

	// ── Metrics ────────────────────────────────────────────────
	var collector *metrics.Collector
	var metricsSrv *metrics.Server
	if cfg.Metrics.Enabled {
		collector = metrics.NewCollector(log)
		metricsSrv = metrics.NewServer(cfg.Metrics.Address, collector, log)
		metricsSrv.Start()
	}

	// ── Notifications ──────────────────────────────────────────
	notifier := notify.New(cfg.Notify, log)

	// ── Report builder ─────────────────────────────────────────
	var reportBuilder *report.Builder
	if cfg.Report.Enabled {
		reportBuilder = report.NewBuilder(log, cfg.DryRun)
	}

	// ── Health checker ─────────────────────────────────────────
	checker := healthcheck.New(cfg.HealthCheck, log)

	// ── Create the chaos engine ────────────────────────────────
	engine := chaos.NewEngine(cfg, log)

	// Wire up callbacks: metrics, reports, and notifications fire after each experiment
	engine.OnResult(func(r chaos.ExperimentResult) {
		// Metrics
		if collector != nil {
			collector.IncCounter("yacmo_experiments_total")
			if r.Success {
				collector.IncCounter("yacmo_experiments_success")
			} else {
				collector.IncCounter("yacmo_experiments_failed")
			}
			collector.SetGauge("yacmo_last_experiment_duration_seconds", r.Duration.Seconds())
		}
		// Report
		if reportBuilder != nil {
			reportBuilder.RecordExperiment(r.ExperimentName, r.StartedAt, r.Duration, r.Error, r.Details)
		}
		// Notification
		status := "✓ success"
		if !r.Success {
			status = "✗ failed"
		}
		notifier.Send(context.Background(), notify.EventExperimentDone,
			fmt.Sprintf("%s — %s (%s)", r.ExperimentName, status, r.Duration),
			nil)
	})

	// ── Register experiments ───────────────────────────────────

	// Kubernetes chaos
	if cfg.Kubernetes.Enabled {
		k8sChaos, err := k8s.New(cfg.Kubernetes, policy, log)
		if err != nil {
			log.Error("Failed to initialize Kubernetes chaos: %v", err)
			os.Exit(1)
		}
		engine.Register(k8sChaos)
	}

	// HTTP injection
	if cfg.HTTP.Enabled {
		httpChaos := httpflood.New(cfg.HTTP, log)
		engine.Register(httpChaos)
	}

	// gRPC injection
	if cfg.GRPC.Enabled {
		grpcChaos := grpcflood.New(cfg.GRPC, log)
		engine.Register(grpcChaos)
	}

	// MQ injection
	if cfg.MQ.Enabled {
		mqChaos := mqflood.New(cfg.MQ, log)
		engine.Register(mqChaos)
	}

	// Network chaos
	if cfg.Network.Enabled {
		netChaos := network.New(cfg.Network, policy, log)
		engine.Register(netChaos)
	}

	// Resource stress
	if cfg.Stress.Enabled {
		stressChaos := stress.New(cfg.Stress, policy, log)
		engine.Register(stressChaos)
	}

	// ── Signal handling ────────────────────────────────────────
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Warn("Received signal %v, initiating graceful shutdown...", sig)
		cancel()
	}()

	// ── Pre-chaos health checks ────────────────────────────────
	beforeHealth := checker.RunAll(ctx, "pre-chaos")

	// ── Notify start ───────────────────────────────────────────
	notifier.Send(ctx, notify.EventChaosStarting, "YACMO chaos run starting", nil)

	// ── Run experiments via scheduler ──────────────────────────
	sched := scheduler.New(cfg.Scheduler, engine, log, cfg.Interval)

	log.Info("Starting YACMO chaos engine...")
	if err := sched.Run(ctx); err != nil && err != context.Canceled {
		log.Error("Scheduler error: %v", err)
		notifier.Send(context.Background(), notify.EventChaosError,
			fmt.Sprintf("Scheduler error: %v", err), nil)
	}

	// ── Post-chaos health checks ───────────────────────────────
	afterHealth := checker.RunAll(context.Background(), "post-chaos")
	checker.CompareResults(beforeHealth, afterHealth)

	// ── Print summary ──────────────────────────────────────────
	engine.PrintSummary()

	// ── Notify completion ──────────────────────────────────────
	results := engine.Results()
	succeeded := 0
	for _, r := range results {
		if r.Success {
			succeeded++
		}
	}
	notifier.Send(context.Background(), notify.EventChaosCompleted,
		fmt.Sprintf("Completed: %d/%d experiments succeeded", succeeded, len(results)), nil)

	// ── Generate report ────────────────────────────────────────
	if reportBuilder != nil {
		rpt := reportBuilder.Build()
		if _, err := report.WriteJSON(rpt, cfg.Report.OutputDir, log); err != nil {
			log.Error("Failed to write report: %v", err)
		}
	}

	// ── Rollback ───────────────────────────────────────────────
	log.Info("Performing rollback (best-effort)...")
	rollbackCtx, rollbackCancel := context.WithTimeout(context.Background(), cfg.Interval)
	defer rollbackCancel()
	engine.RollbackAll(rollbackCtx)

	// ── Shutdown metrics server ────────────────────────────────
	if metricsSrv != nil {
		metricsSrv.Shutdown(context.Background())
	}

	log.Info("YACMO finished. Goodbye! 🐒")
}

func promptForApproval(stdin *os.File, log *logger.Logger) bool {
	info, err := stdin.Stat()
	if err != nil || info.Mode()&os.ModeCharDevice == 0 {
		return false
	}

	fmt.Print("Safety approval required. Type YES to continue: ")
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false
	}
	if strings.EqualFold(strings.TrimSpace(line), "YES") {
		return true
	}
	log.Warn("Safety approval declined")
	return false
}
