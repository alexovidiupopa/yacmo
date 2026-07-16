// Package config provides configuration structures and loading for YACMO (Yet Another Chaos Monkey).
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"time"
)

// Config is the top-level configuration for YACMO.
type Config struct {
	// General settings
	DryRun   bool          `json:"dry_run"`
	LogLevel string        `json:"log_level"` // debug, info, warn, error
	Interval time.Duration `json:"interval"`  // interval between chaos actions

	// Kubernetes chaos configuration
	Kubernetes KubernetesConfig `json:"kubernetes"`

	// HTTP injection configuration
	HTTP HTTPConfig `json:"http"`

	// gRPC injection configuration
	GRPC GRPCConfig `json:"grpc"`

	// MQ injection configuration
	MQ MQConfig `json:"mq"`

	// Network chaos configuration
	Network NetworkConfig `json:"network"`

	// Resource stress configuration
	Stress StressConfig `json:"stress"`

	// Scheduler configuration
	Scheduler SchedulerConfig `json:"scheduler"`

	// Metrics server configuration
	Metrics MetricsConfig `json:"metrics"`

	// Report generation configuration
	Report ReportConfig `json:"report"`

	// Webhook notification configuration
	Notify NotifyConfig `json:"notify"`

	// Health check configuration
	HealthCheck HealthCheckConfig `json:"healthcheck"`

	// Safety configuration
	Safety SafetyConfig `json:"safety"`
	// Scenarios allow named orchestration of experiments (ordering, groups, prerequisites, retries, conditional steps)
	Scenarios []Scenario `json:"scenarios"`
}

// KubernetesConfig holds settings for Kubernetes chaos experiments.
type KubernetesConfig struct {
	Enabled    bool     `json:"enabled"`
	Kubeconfig string   `json:"kubeconfig"` // path to kubeconfig, empty for in-cluster
	Namespaces []string `json:"namespaces"` // target namespaces
	// Label selectors to target specific pods/services
	LabelSelectors []string `json:"label_selectors"`
	// Actions: "kill_pod", "stop_pod", "scale_down", "delete_service"
	Actions []string `json:"actions"`
	// MaxTargets limits how many resources to affect per round
	MaxTargets int `json:"max_targets"`
	// GracePeriodSeconds for pod termination
	GracePeriodSeconds int64 `json:"grace_period_seconds"`
	// ExcludedPods are pod name patterns to never touch
	ExcludedPods []string `json:"excluded_pods"`
}

// HTTPConfig holds settings for HTTP traffic injection.
type HTTPConfig struct {
	Enabled bool         `json:"enabled"`
	Targets []HTTPTarget `json:"targets"`
}

// HTTPTarget defines a single HTTP injection target.
type HTTPTarget struct {
	Name    string            `json:"name"`
	URL     string            `json:"url"`
	Method  string            `json:"method"` // GET, POST, PUT, DELETE, PATCH
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	// Chaos parameters
	Concurrency    int           `json:"concurrency"`     // number of concurrent requests
	TotalRequests  int           `json:"total_requests"`  // total number of requests to send
	RatePerSecond  float64       `json:"rate_per_second"` // rate limit
	TimeoutSeconds int           `json:"timeout_seconds"`
	Duration       time.Duration `json:"duration"` // how long to run the injection
	// Fault injection
	InjectLatency    time.Duration `json:"inject_latency"`     // artificial latency to add
	InjectErrorRatio float64       `json:"inject_error_ratio"` // ratio of requests to abort (0.0-1.0)
	// Payload mutation
	RandomizeBody bool `json:"randomize_body"`  // generate random payloads
	BodySizeBytes int  `json:"body_size_bytes"` // size of random payload
}

// GRPCConfig holds settings for gRPC traffic injection.
type GRPCConfig struct {
	Enabled bool         `json:"enabled"`
	Targets []GRPCTarget `json:"targets"`
}

// GRPCTarget defines a single gRPC injection target.
type GRPCTarget struct {
	Name     string            `json:"name"`
	Address  string            `json:"address"`  // host:port
	Method   string            `json:"method"`   // full gRPC method e.g. "/package.Service/Method"
	Insecure bool              `json:"insecure"` // use plaintext (no TLS)
	Metadata map[string]string `json:"metadata"` // gRPC metadata (headers)
	// Payload
	Payload          string `json:"payload"`            // static payload
	RandomPayload    bool   `json:"random_payload"`     // generate random payloads
	PayloadSizeBytes int    `json:"payload_size_bytes"` // size of random payload
	// Chaos parameters
	Concurrency   int           `json:"concurrency"`
	TotalRequests int           `json:"total_requests"`
	RatePerSecond float64       `json:"rate_per_second"`
	Duration      time.Duration `json:"duration"`
}

// MQConfig holds settings for message queue traffic injection.
type MQConfig struct {
	Enabled  bool       `json:"enabled"`
	Backends []MQTarget `json:"backends"`
}

// MQTarget defines a single MQ injection target.
type MQTarget struct {
	Name string `json:"name"`
	// Type: "rabbitmq", "kafka", "nats"
	Type string `json:"type"`
	// Connection
	BrokerURL string `json:"broker_url"`
	Topic     string `json:"topic"`
	Queue     string `json:"queue"`
	// Authentication
	Username string `json:"username"`
	Password string `json:"password"`
	// Chaos parameters
	MessageCount int           `json:"message_count"`
	MessageSize  int           `json:"message_size"` // bytes
	Concurrency  int           `json:"concurrency"`
	Duration     time.Duration `json:"duration"`
	RatePerSec   float64       `json:"rate_per_sec"`
	// Payload
	RandomPayload  bool   `json:"random_payload"`
	PayloadPattern string `json:"payload_pattern"` // template for messages
}

// NetworkConfig holds settings for network-level chaos.
type NetworkConfig struct {
	Enabled   bool     `json:"enabled"`
	Interface string   `json:"interface"` // network interface (e.g. "eth0")
	Actions   []string `json:"actions"`   // "latency", "packet_loss", "dns_failure", "bandwidth_limit", "corrupt"
	// Parameters
	LatencyMs          int           `json:"latency_ms"`
	JitterMs           int           `json:"jitter_ms"`
	PacketLossPercent  float64       `json:"packet_loss_percent"`
	CorruptPercent     float64       `json:"corrupt_percent"`
	BandwidthLimitKbps int           `json:"bandwidth_limit_kbps"`
	Duration           time.Duration `json:"duration"` // how long to keep the chaos active
}

// StressConfig holds settings for resource stress experiments.
type StressConfig struct {
	Enabled  bool          `json:"enabled"`
	Actions  []string      `json:"actions"` // "cpu", "memory", "disk_io", "disk_fill"
	Duration time.Duration `json:"duration"`
	// CPU stress
	CPUCores int `json:"cpu_cores"` // 0 = all cores
	// Memory stress
	MemoryMB int `json:"memory_mb"`
	// Disk I/O stress
	DiskIOWorkers int    `json:"disk_io_workers"`
	TempDir       string `json:"temp_dir"` // directory for temp files
	// Disk fill
	DiskFillMB int `json:"disk_fill_mb"`
}

// MetricsConfig holds settings for the Prometheus metrics server.
type MetricsConfig struct {
	Enabled bool   `json:"enabled"`
	Address string `json:"address"` // e.g. ":9090"
}

// ReportConfig holds settings for report generation.
type ReportConfig struct {
	Enabled   bool   `json:"enabled"`
	OutputDir string `json:"output_dir"` // directory to write reports to
	Format    string `json:"format"`     // output format (json, html, csv)
}

// NotifyConfig holds settings for webhook notifications.
type NotifyConfig struct {
	Enabled  bool            `json:"enabled"`
	Webhooks []WebhookTarget `json:"webhooks"`
}

// WebhookTarget defines a single notification endpoint.
type WebhookTarget struct {
	Name    string            `json:"name"`
	Type    string            `json:"type"` // "slack", "discord", "generic"
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// HealthCheckConfig holds settings for pre/post-experiment health probes.
type HealthCheckConfig struct {
	Enabled        bool             `json:"enabled"`
	TimeoutSeconds int              `json:"timeout_seconds"`
	Endpoints      []HealthEndpoint `json:"endpoints"`
}

// HealthEndpoint defines a single health check target.
type HealthEndpoint struct {
	Name           string `json:"name"`
	URL            string `json:"url"`
	Method         string `json:"method"`          // GET, HEAD, etc.
	ExpectedStatus int    `json:"expected_status"` // expected HTTP status code (default 200)
}

// SchedulerConfig controls when chaos experiments run.
type SchedulerConfig struct {
	// Mode: "continuous", "cron", "once"
	Mode string `json:"mode"`
	// CronExpression for cron mode (e.g. "*/5 * * * *")
	CronExpression string `json:"cron_expression"`
	// MaxExperiments limits total experiments before stopping (0 = unlimited)
	MaxExperiments int `json:"max_experiments"`
}

// SafetyConfig controls safety guardrails for destructive experiments.
type SafetyConfig struct {
	Enabled                     bool     `json:"enabled"`
	FailClosed                  bool     `json:"fail_closed"`
	RequireApproval             bool     `json:"require_approval"`
	InteractiveConfirm          bool     `json:"interactive_confirm"`
	AllowDestructiveActions     bool     `json:"allow_destructive_actions"`
	AllowedNamespaces           []string `json:"allowed_namespaces"`
	BlockedNamespaces           []string `json:"blocked_namespaces"`
	AllowedNamePatterns         []string `json:"allowed_name_patterns"`
	BlockedNamePatterns         []string `json:"blocked_name_patterns"`
	MaxTargetsPerRun            int      `json:"max_targets_per_run"`
	MaxDestructiveActionsPerRun int      `json:"max_destructive_actions_per_run"`
}

// DefaultConfig returns a sensible default configuration.
func DefaultConfig() *Config {
	return &Config{
		DryRun:   true,
		LogLevel: "info",
		Interval: 30 * time.Second,
		Kubernetes: KubernetesConfig{
			Enabled:            false,
			Namespaces:         []string{"default"},
			Actions:            []string{"kill_pod"},
			MaxTargets:         1,
			GracePeriodSeconds: 30,
		},
		HTTP: HTTPConfig{
			Enabled: false,
		},
		GRPC: GRPCConfig{
			Enabled: false,
		},
		MQ: MQConfig{
			Enabled: false,
		},
		Network: NetworkConfig{
			Enabled:   false,
			Interface: "eth0",
		},
		Stress: StressConfig{
			Enabled:  false,
			Duration: 30 * time.Second,
		},
		Scheduler: SchedulerConfig{
			Mode: "once",
		},
		Metrics: MetricsConfig{
			Enabled: false,
			Address: ":9090",
		},
		Report: ReportConfig{
			Enabled:   false,
			OutputDir: "./reports",
			Format:    "json",
		},
		Notify: NotifyConfig{
			Enabled: false,
		},
		HealthCheck: HealthCheckConfig{
			Enabled:        false,
			TimeoutSeconds: 5,
		},
		Safety: SafetyConfig{
			Enabled:                     true,
			FailClosed:                  true,
			RequireApproval:             true,
			InteractiveConfirm:          true,
			AllowDestructiveActions:     false,
			BlockedNamespaces:           []string{"kube-system", "kube-public"},
			BlockedNamePatterns:         []string{`^kube-.*`, `^istio-.*`, `^linkerd-.*`},
			MaxTargetsPerRun:            20,
			MaxDestructiveActionsPerRun: 8,
		},
		Scenarios: []Scenario{},
	}
}

// Scenario defines a named sequence/group of steps referencing registered experiments.
type Scenario struct {
	Name          string         `json:"name"`
	Enabled       bool           `json:"enabled"`
	Order         int            `json:"order"` // lower order runs first
	Parallel      bool           `json:"parallel"`
	Prerequisites []string       `json:"prerequisites"` // scenario names that must have succeeded
	Retries       int            `json:"retries"`       // default retries for steps in this scenario
	Steps         []ScenarioStep `json:"steps"`
}

// ScenarioStep references a named experiment and supports per-step condition and retries.
type ScenarioStep struct {
	Name      string `json:"name"`      // experiment id registered via Engine.RegisterNamed
	Condition string `json:"condition"` // "always" (default), "on_success", "on_failure"
	Retries   int    `json:"retries"`   // per-step retries override
}

// LoadFromFile reads configuration from a JSON file.
func LoadFromFile(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	cfg := DefaultConfig()
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	return cfg, nil
}

// Validate checks that the configuration is consistent and usable.
func (c *Config) Validate() error {
	if c.Kubernetes.Enabled {
		if len(c.Kubernetes.Namespaces) == 0 {
			return fmt.Errorf("kubernetes enabled but no namespaces specified")
		}
		if len(c.Kubernetes.Actions) == 0 {
			return fmt.Errorf("kubernetes enabled but no actions specified")
		}
	}

	if c.HTTP.Enabled {
		if len(c.HTTP.Targets) == 0 {
			return fmt.Errorf("http enabled but no targets specified")
		}
		for i, t := range c.HTTP.Targets {
			if t.URL == "" {
				return fmt.Errorf("http target %d has no URL", i)
			}
		}
	}

	if c.GRPC.Enabled {
		if len(c.GRPC.Targets) == 0 {
			return fmt.Errorf("grpc enabled but no targets specified")
		}
		for i, t := range c.GRPC.Targets {
			if t.Address == "" {
				return fmt.Errorf("grpc target %d has no address", i)
			}
			if t.Method == "" {
				return fmt.Errorf("grpc target %d has no method", i)
			}
		}
	}

	if c.MQ.Enabled {
		if len(c.MQ.Backends) == 0 {
			return fmt.Errorf("mq enabled but no backends specified")
		}
		for i, b := range c.MQ.Backends {
			if b.BrokerURL == "" {
				return fmt.Errorf("mq backend %d has no broker_url", i)
			}
			switch b.Type {
			case "rabbitmq", "kafka", "nats":
			default:
				return fmt.Errorf("mq backend %d has unsupported type %q (use rabbitmq, kafka, or nats)", i, b.Type)
			}
		}
	}

	if c.Network.Enabled {
		if c.Network.Interface == "" {
			return fmt.Errorf("network enabled but no interface specified")
		}
		if len(c.Network.Actions) == 0 {
			return fmt.Errorf("network enabled but no actions specified")
		}
	}

	if c.Stress.Enabled {
		if len(c.Stress.Actions) == 0 {
			return fmt.Errorf("stress enabled but no actions specified")
		}
	}

	if c.Notify.Enabled {
		for i, wh := range c.Notify.Webhooks {
			if wh.URL == "" {
				return fmt.Errorf("webhook %d has no URL", i)
			}
		}
	}

	if c.HealthCheck.Enabled {
		for i, ep := range c.HealthCheck.Endpoints {
			if ep.URL == "" {
				return fmt.Errorf("healthcheck endpoint %d has no URL", i)
			}
		}
	}

	if c.Safety.Enabled {
		for i, pattern := range c.Safety.AllowedNamePatterns {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("safety allowed_name_patterns[%d] is invalid: %w", i, err)
			}
		}
		for i, pattern := range c.Safety.BlockedNamePatterns {
			if _, err := regexp.Compile(pattern); err != nil {
				return fmt.Errorf("safety blocked_name_patterns[%d] is invalid: %w", i, err)
			}
		}
		if c.Safety.MaxTargetsPerRun < 0 {
			return fmt.Errorf("safety max_targets_per_run cannot be negative")
		}
		if c.Safety.MaxDestructiveActionsPerRun < 0 {
			return fmt.Errorf("safety max_destructive_actions_per_run cannot be negative")
		}
	}

	return nil
}
