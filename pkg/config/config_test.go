package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.DryRun {
		t.Error("DefaultConfig should be dry-run by default (safe default)")
	}
	if !cfg.Safety.Enabled {
		t.Error("DefaultConfig should have safety enabled by default")
	}
	if cfg.Safety.AllowDestructiveActions {
		t.Error("DefaultConfig should not allow destructive actions by default")
	}
	if cfg.Interval != 30*time.Second {
		t.Errorf("Interval = %v, want 30s", cfg.Interval)
	}
	if cfg.Scheduler.Mode != "once" {
		t.Errorf("Scheduler.Mode = %q, want %q", cfg.Scheduler.Mode, "once")
	}
	// A fresh default config must validate.
	if err := cfg.Validate(); err != nil {
		t.Errorf("DefaultConfig().Validate() returned error: %v", err)
	}
}

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	// Only a subset of fields is set; the rest must keep DefaultConfig values.
	content := `{
		"dry_run": false,
		"log_level": "debug",
		"interval": 5000000000,
		"http": {
			"enabled": true,
			"targets": [{"name": "t1", "url": "http://example.com"}]
		}
	}`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}

	cfg, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("LoadFromFile returned error: %v", err)
	}

	if cfg.DryRun {
		t.Error("DryRun should be false after load")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", cfg.Interval)
	}
	if !cfg.HTTP.Enabled || len(cfg.HTTP.Targets) != 1 || cfg.HTTP.Targets[0].URL != "http://example.com" {
		t.Errorf("HTTP config not loaded correctly: %+v", cfg.HTTP)
	}
	// Unspecified safety block must retain defaults.
	if !cfg.Safety.Enabled {
		t.Error("Safety.Enabled should retain its default (true) when not overridden")
	}
}

func TestLoadFromFileErrors(t *testing.T) {
	if _, err := LoadFromFile(filepath.Join(t.TempDir(), "does-not-exist.json")); err == nil {
		t.Error("LoadFromFile of a missing file should return an error")
	}

	dir := t.TempDir()
	badPath := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(badPath, []byte("{not valid json"), 0644); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	if _, err := LoadFromFile(badPath); err == nil {
		t.Error("LoadFromFile of malformed JSON should return an error")
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(c *Config)
		wantErr bool
	}{
		{
			name:    "default is valid",
			mutate:  func(c *Config) {},
			wantErr: false,
		},
		{
			name: "kubernetes enabled without namespaces",
			mutate: func(c *Config) {
				c.Kubernetes.Enabled = true
				c.Kubernetes.Namespaces = nil
				c.Kubernetes.Actions = []string{"kill_pod"}
			},
			wantErr: true,
		},
		{
			name: "kubernetes enabled without actions",
			mutate: func(c *Config) {
				c.Kubernetes.Enabled = true
				c.Kubernetes.Namespaces = []string{"default"}
				c.Kubernetes.Actions = nil
			},
			wantErr: true,
		},
		{
			name: "kubernetes fully specified",
			mutate: func(c *Config) {
				c.Kubernetes.Enabled = true
				c.Kubernetes.Namespaces = []string{"default"}
				c.Kubernetes.Actions = []string{"kill_pod"}
			},
			wantErr: false,
		},
		{
			name: "http enabled without targets",
			mutate: func(c *Config) {
				c.HTTP.Enabled = true
				c.HTTP.Targets = nil
			},
			wantErr: true,
		},
		{
			name: "http target missing url",
			mutate: func(c *Config) {
				c.HTTP.Enabled = true
				c.HTTP.Targets = []HTTPTarget{{Name: "t"}}
			},
			wantErr: true,
		},
		{
			name: "grpc target missing address",
			mutate: func(c *Config) {
				c.GRPC.Enabled = true
				c.GRPC.Targets = []GRPCTarget{{Name: "g", Method: "/svc/M"}}
			},
			wantErr: true,
		},
		{
			name: "grpc target missing method",
			mutate: func(c *Config) {
				c.GRPC.Enabled = true
				c.GRPC.Targets = []GRPCTarget{{Name: "g", Address: "host:1"}}
			},
			wantErr: true,
		},
		{
			name: "mq unsupported type",
			mutate: func(c *Config) {
				c.MQ.Enabled = true
				c.MQ.Backends = []MQTarget{{Name: "m", Type: "sqs", BrokerURL: "x"}}
			},
			wantErr: true,
		},
		{
			name: "mq missing broker url",
			mutate: func(c *Config) {
				c.MQ.Enabled = true
				c.MQ.Backends = []MQTarget{{Name: "m", Type: "kafka"}}
			},
			wantErr: true,
		},
		{
			name: "mq valid",
			mutate: func(c *Config) {
				c.MQ.Enabled = true
				c.MQ.Backends = []MQTarget{{Name: "m", Type: "kafka", BrokerURL: "localhost:9092"}}
			},
			wantErr: false,
		},
		{
			name: "network enabled without actions",
			mutate: func(c *Config) {
				c.Network.Enabled = true
				c.Network.Interface = "eth0"
				c.Network.Actions = nil
			},
			wantErr: true,
		},
		{
			name: "network enabled without interface",
			mutate: func(c *Config) {
				c.Network.Enabled = true
				c.Network.Interface = ""
				c.Network.Actions = []string{"latency"}
			},
			wantErr: true,
		},
		{
			name: "stress enabled without actions",
			mutate: func(c *Config) {
				c.Stress.Enabled = true
				c.Stress.Actions = nil
			},
			wantErr: true,
		},
		{
			name: "notify webhook missing url",
			mutate: func(c *Config) {
				c.Notify.Enabled = true
				c.Notify.Webhooks = []WebhookTarget{{Name: "w"}}
			},
			wantErr: true,
		},
		{
			name: "healthcheck endpoint missing url",
			mutate: func(c *Config) {
				c.HealthCheck.Enabled = true
				c.HealthCheck.Endpoints = []HealthEndpoint{{Name: "h"}}
			},
			wantErr: true,
		},
		{
			name: "safety invalid allowed name pattern",
			mutate: func(c *Config) {
				c.Safety.AllowedNamePatterns = []string{"("}
			},
			wantErr: true,
		},
		{
			name: "safety invalid blocked name pattern",
			mutate: func(c *Config) {
				c.Safety.BlockedNamePatterns = []string{"["}
			},
			wantErr: true,
		},
		{
			name: "safety negative max targets",
			mutate: func(c *Config) {
				c.Safety.MaxTargetsPerRun = -1
			},
			wantErr: true,
		},
		{
			name: "safety negative max destructive actions",
			mutate: func(c *Config) {
				c.Safety.MaxDestructiveActionsPerRun = -1
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultConfig()
			tt.mutate(cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
