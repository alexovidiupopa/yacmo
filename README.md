# YACMO — Yet Another Chaos Monkey

> A comprehensive chaos engineering toolkit written in Go for Kubernetes environments and beyond.

```
██╗   ██╗ █████╗  ██████╗███╗   ███╗ ██████╗
╚██╗ ██╔╝██╔══██╗██╔════╝████╗ ████║██╔═══██╗
 ╚████╔╝ ███████║██║     ██╔████╔██║██║   ██║
  ╚██╔╝  ██╔══██║██║     ██║╚██╔╝██║██║   ██║
   ██║   ██║  ██║╚██████╗██║ ╚═╝ ██║╚██████╔╝
   ╚═╝   ╚═╝  ╚═╝ ╚═════╝╚═╝     ╚═╝ ╚═════╝
```

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Features](#features)
- [Getting Started](#getting-started)
- [Configuration Reference](#configuration-reference)
- [Scenario Orchestration](#scenario-orchestration)
- [Safety Mechanisms](#safety-mechanisms)
- [Safety Configuration](#safety-configuration)
- [Example Scenarios](#example-scenarios)

---

## Overview

YACMO is a chaos engineering tool designed to test the resilience of distributed systems. It orchestrates controlled failure injection across multiple layers of your infrastructure:

| Layer | Capabilities |
|---|---|
| **Kubernetes** | Kill pods, scale down deployments, delete services |
| **HTTP** | Traffic floods, latency injection, payload mutation |
| **gRPC** | Concurrent RPC floods with metadata and payload control |
| **Message Queues** | RabbitMQ, Kafka, NATS — flood with configurable payloads |
| **Network** | Latency, packet loss, DNS failure, bandwidth limits, corruption |
| **System Resources** | CPU burn, memory pressure, disk I/O stress, disk fill |

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                         main.go                             │
│  CLI flags · config loading · signal handling · wiring      │
└──────────────────────────┬──────────────────────────────────┘
                           │
           ┌───────────────▼───────────────┐
           │        Scheduler              │
           │  once · continuous · cron     │
           └───────────────┬───────────────┘
                           │
           ┌───────────────▼──────────────────────────┐
           │        Chaos Engine                      │
           │  Scenario orchestration:                 │
           │  · Named experiments                     │
           │  · Sequential/parallel steps             │
           │  · Prerequisites & dependencies          │
           │  · Retries & conditional execution       │
           │  · Result callbacks & rollback           │
           └──┬──┬──┬──┬──┬──┬───────────────────────┘
              │  │  │  │  │  │
     ┌────────┘  │  │  │  │  └────────┐
     ▼           ▼  ▼  ▼  ▼           ▼
 ┌──────┐  ┌────┐┌────┐┌──┐  ┌───────┐┌──────┐
 │ K8s  │  │HTTP││gRPC││MQ│  │Network││Stress│
 └──────┘  └────┘└────┘└──┘  └───────┘└──────┘

     ┌──────────────────────────────────────┐
     │      Cross-cutting concerns          │
     │  Metrics · Reports · Notifications   │
     │  Health Checks · Logging · Safety    │
     └──────────────────────────────────────┘
```

### Package Layout

```
yacmo/
├── main.go                        # Entry point — wires everything together
├── config.example.json            # Full example configuration
├── pkg/
│   ├── chaos/engine.go            # Core engine: experiment registration, execution, rollback
│   ├── config/config.go           # Configuration types, loading, validation
│   ├── logger/logger.go           # Leveled logger (debug/info/warn/error)
│   ├── scheduler/scheduler.go     # Scheduling: once, continuous, cron
│   │
│   ├── k8s/k8s.go                # Kubernetes chaos (pods, deployments, services)
│   ├── httpflood/httpflood.go     # HTTP traffic injection
│   ├── grpcflood/grpcflood.go     # gRPC traffic injection
│   ├── mqflood/                   # Message queue injection
│   │   ├── mqflood.go             #   Orchestrator + MQProducer interface
│   │   ├── amqp_producer.go       #   RabbitMQ (AMQP 0-9-1)
│   │   ├── kafka_producer.go      #   Apache Kafka
│   │   └── nats_producer.go       #   NATS
│   ├── network/network.go         # Network chaos (tc/iptables)
│   ├── stress/stress.go           # Resource stress (CPU, memory, disk)
│   │
│   ├── metrics/metrics.go         # Prometheus metrics server
│   ├── report/report.go           # JSON/HTML/CSV report generation
│   ├── notify/notify.go           # Webhook notifications (Slack, Discord, generic)
│   └── healthcheck/healthcheck.go # Pre/post health probes
```

## Features

### 1. Kubernetes Chaos (`pkg/k8s/`)

| Action | Description |
|---|---|
| `kill_pod` | Randomly selects and deletes pods (respects grace period) |
| `scale_down` | Scales deployments to 0 replicas (rollback restores original count) |
| `delete_service` | Removes services (auto-protects the built-in `kubernetes` service) |

- **Label selectors** to target specific workloads
- **Exclusion patterns** to protect critical pods
- **`MaxTargets`** cap to limit blast radius per round
- In-cluster and out-of-cluster (kubeconfig) support

### 2. HTTP Traffic Injection (`pkg/httpflood/`)

- Concurrent HTTP flood with configurable worker pool
- Rate limiting (requests/second)
- All HTTP methods (GET, POST, PUT, DELETE, PATCH)
- Custom headers and request bodies
- **Payload mutation** — random binary payloads of configurable size
- **Latency injection** — artificial delay before each request
- Detailed statistics: total requests, success/error count, average latency

### 3. gRPC Traffic Injection (`pkg/grpcflood/`)

- Concurrent unary RPC floods
- Custom gRPC metadata (headers)
- Static or random payload generation
- Rate limiting and duration control
- TLS and plaintext (insecure) support

### 4. Message Queue Injection (`pkg/mqflood/`)

| Backend | Protocol | Features |
|---|---|---|
| **RabbitMQ** | AMQP 0-9-1 | Auto queue declaration, routing keys |
| **Apache Kafka** | Kafka protocol | SASL authentication, sync producer |
| **NATS** | NATS protocol | Lightweight pub, user/pass auth |

All backends support:
- Concurrent producers with rate limiting
- Random or pattern-based payload generation
- Configurable message count and size

### 5. Network Chaos (`pkg/network/`)

| Action | Tool | Description |
|---|---|---|
| `latency` | `tc netem` | Adds delay with optional jitter |
| `packet_loss` | `tc netem` | Drops a percentage of packets |
| `dns_failure` | `iptables` | Blocks UDP/TCP port 53 |
| `bandwidth_limit` | `tc tbf` | Throttles bandwidth (kbps) |
| `corrupt` | `tc netem` | Corrupts a percentage of packets |

- Auto-rollback after configurable duration
- Full rollback support (removes all injected rules)
- Requires Linux with `tc` and `iptables` available

### 6. Resource Stress (`pkg/stress/`)

| Action | Description |
|---|---|
| `cpu` | Burns CPU with tight loops across N cores |
| `memory` | Allocates and holds N MB (touches every page) |
| `disk_io` | Parallel random read/write workers |
| `disk_fill` | Creates a large temp file to consume disk space |

- All stress actions run concurrently with a shared duration limit
- Temp files are tracked and cleaned up on rollback

### 7. Health Check Probes (`pkg/healthcheck/`)

- Probes HTTP endpoints **before** and **after** chaos
- Compares results to detect **degradation** and **recovery**
- Configurable expected status codes and timeouts
- Logs latency delta for each endpoint

### 8. Prometheus Metrics (`pkg/metrics/`)

- Serves `/metrics` in Prometheus text exposition format
- Also serves `/healthz` for liveness probes
- Tracks:
  - `yacmo_experiments_total` — total experiments run
  - `yacmo_experiments_success` — successful experiments
  - `yacmo_experiments_failed` — failed experiments
  - `yacmo_last_experiment_duration_seconds` — duration of the last experiment

### 9. JSON Reports (`pkg/report/`)

- Generates timestamped JSON/HTML/CSV reports after each run
- Includes: version, dry-run flag, total duration, per-experiment status/timing/errors
- Output: `./reports/yacmo-report-20260228-143022.json`

### 10. Webhook Notifications (`pkg/notify/`)

| Type | Format |
|---|---|
| `slack` | `{"text": "🐒 [YACMO] ..."}` |
| `discord` | `{"content": "🐒 [YACMO] ..."}` |
| `generic` | Full JSON payload with event, timestamp, message, details |

Events emitted: `chaos_starting`, `chaos_completed`, `experiment_done`, `chaos_error`

### 11. Scheduling (`pkg/scheduler/`)

| Mode | Description |
|---|---|
| `once` | Run all experiments a single time |
| `continuous` | Repeat on a fixed interval with optional max rounds |
| `cron` | Full cron scheduling via [robfig/cron](https://github.com/robfig/cron) |

The `cron` mode accepts standard 5-field cron expressions (e.g. `0 */2 * * *`
for every two hours), the `@every <duration>` form (e.g. `@every 90s`,
`@every 1h30m`), and named descriptors (`@hourly`, `@daily`, `@weekly`,
`@monthly`, `@yearly`). Invalid expressions are rejected at config load.

---

## Getting Started

### Build

```bash
go build -o yacmo .
```

For a versioned build, stamp the version in via `-ldflags` (this is what CI does):

```bash
go build -ldflags "-X yacmo/pkg/version.Version=1.4.0" -o yacmo .
./yacmo -version   # YACMO 1.4.0 — Yet Another Chaos Monkey (...)
```

### Releases

CI ([.github/workflows/go.yml](.github/workflows/go.yml)) builds and tests on every
push/PR to `master`, uploading the cross-compiled binaries and the test results
(JUnit + coverage) as downloadable run artifacts. To cut a release:

- Push a `release/X.Y.Z` branch — CI derives an `X.Y.Z-rc.<run>` version, builds the
  versioned binaries, and publishes a **pre-release** you can validate.
- Push a `vX.Y.Z` tag — CI publishes a full **GitHub Release** with the binaries attached.

### Run

```bash
# Dry-run with example config
cp config.example.json config.json
./yacmo -config config.json -dry-run

# Real execution
./yacmo -config config.json

# Override log level
./yacmo -config config.json -log-level debug
```

### CLI Flags

| Flag | Default | Description |
|---|---|---|
| `-config` | `config.json` | Path to configuration file |
| `-dry-run` | `false` | Log what would happen without executing |
| `-log-level` | (from config) | Override: `debug`, `info`, `warn`, `error` |
| `-approve` | `false` | Approve destructive actions after safety preflight |
| `-version` | — | Print version and exit |

---

## Configuration Reference

See [`config.example.json`](config_examples/config-dry-run.json) for a complete example.

All durations are in nanoseconds in JSON (Go's `time.Duration` encoding).
Common values: `30s = 30000000000`, `1m = 60000000000`, `5m = 300000000000`.

### Top-Level

| Field | Type | Description |
|---|---|---|
| `dry_run` | bool | Skip actual execution, only log |
| `log_level` | string | `debug`, `info`, `warn`, `error` |
| `interval` | duration | Interval between scheduler rounds |
| `safety` | object | Guardrails for namespaces, patterns, approval, and blast-radius caps |

### Feature Sections

Each module has an `"enabled": true/false` toggle. See `config.example.json` for all fields.

---

## Safety Mechanisms

1. **Dry-run mode** (`-dry-run`) — logs all actions without executing them
2. **Exclusion patterns** — protect critical pods/services by name pattern
3. **MaxTargets** — cap how many resources are affected per round
4. **Graceful shutdown** — catches SIGINT/SIGTERM, cancels in-flight work
5. **Automatic rollback** — restores scaled deployments, removes network rules, cleans temp files
6. **Protected resources** — never deletes the `kubernetes` service
7. **Health check comparison** — measures blast radius before/after chaos
8. **Duration limits** — all experiments respect context cancellation and duration caps
9. **Safety preflight** — validates approval, allow/deny rules, and command availability before running destructive actions
10. **Scenario prerequisites** — ensures experiments only run after dependencies succeed
11. **Resource quotas** — enforces safety caps on destructive actions per run

---

## Safety Configuration

The `safety` section in your config controls guardrails for destructive experiments:

```json
{
  "safety": {
    "enabled": true,
    "fail_closed": true,
    "require_approval": false,
    "interactive_confirm": true,
    "allow_destructive_actions": true,
    "allowed_namespaces": ["default", "app-tier"],
    "blocked_namespaces": ["kube-system", "kube-public"],
    "allowed_name_patterns": [],
    "blocked_name_patterns": ["^kube-.*", "^istio-.*", "^linkerd-.*"],
    "max_targets_per_run": 20,
    "max_destructive_actions_per_run": 8
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `true` | Enable safety guardrails |
| `fail_closed` | bool | `true` | If safety check fails, abort run |
| `require_approval` | bool | `true` | Require explicit approval flag |
| `interactive_confirm` | bool | `true` | Prompt on terminal for YES confirmation |
| `allow_destructive_actions` | bool | `false` | Allow actions like kill_pod, scale_down, delete_service |
| `allowed_namespaces` | []string | `[]` | Whitelist; if non-empty, only these namespaces are targetable |
| `blocked_namespaces` | []string | — | Blacklist namespaces (e.g., kube-system) |
| `allowed_name_patterns` | []string | `[]` | Regex whitelist for resource names; if non-empty, only matching names are targetable |
| `blocked_name_patterns` | []string | — | Regex blacklist for resource names (e.g., `^kube-.*`) |
| `max_targets_per_run` | int | `20` | Maximum resources to target per execution |
| `max_destructive_actions_per_run` | int | `8` | Maximum destructive operations (kills, scales, deletes) per run |

---

## Scenario Orchestration

**Named Scenarios** allow you to orchestrate multi-step chaos campaigns with **ordering, parallel groups, prerequisites, retries, and conditional execution**.

### Scenario Features

- **Named experiments**: Register experiments with stable IDs so scenarios can reference them
  - Registered IDs: `"kubernetes"`, `"http"`, `"grpc"`, `"mq"`, `"network"`, `"stress"`
- **Ordering**: Control execution order via `order` field (lower runs first)
- **Parallel execution**: Set `parallel: true` to run all steps concurrently
- **Prerequisites**: Scenario A can depend on Scenario B (waits for success)
- **Retries**: Configurable at scenario level or per-step
- **Conditional steps**: Run steps based on previous step success/failure

### Scenario Configuration

```json
{
  "scenarios": [
    {
      "name": "baseline-chaos",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "retries": 1,
      "steps": [
        {
          "name": "network",
          "condition": "always",
          "retries": 0
        },
        {
          "name": "http",
          "condition": "on_success",
          "retries": 2
        }
      ]
    },
    {
      "name": "multi-vector-attack",
      "enabled": true,
      "order": 20,
      "parallel": true,
      "prerequisites": ["baseline-chaos"],
      "retries": 0,
      "steps": [
        { "name": "stress", "condition": "always" },
        { "name": "kubernetes", "condition": "always" }
      ]
    }
  ]
}
```

### Scenario Fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Unique scenario identifier (used in logs and prerequisites) |
| `enabled` | bool | Skip if `false` |
| `order` | int | Lower values run first (scenarios sorted by this) |
| `parallel` | bool | If `true`, all steps run concurrently; if `false`, sequentially |
| `prerequisites` | []string | Scenario names that must succeed before this runs |
| `retries` | int | Default retry count for steps in this scenario |
| `steps` | []ScenarioStep | List of experiment steps |

### ScenarioStep Fields

| Field | Type | Description |
|---|---|---|
| `name` | string | Experiment ID: `"kubernetes"`, `"http"`, `"grpc"`, `"mq"`, `"network"`, `"stress"` |
| `condition` | string | `"always"` (default), `"on_success"` (run only if prev succeeded), `"on_failure"` (run only if prev failed) |
| `retries` | int | Per-step retry count (overrides scenario default) |

### Execution Examples

**Sequential Scenario (Default)**
```
Step 1: network chaos
  ↓
Step 2: HTTP injection (only if step 1 succeeded)
```

**Parallel Scenario**
```
Step 1: Stress ─┐
Step 2: K8s    ├─ (concurrent)
```

**With Prerequisites**
```
Scenario A (order 10)
  ↓ (succeeds)
Scenario B (order 20, prerequisite: A)
  ↓ (now runs)
Scenario C (order 30, prerequisite: B)
```

---

## Example Scenarios

### Scenario 1: Kubernetes Resilience Test
> *"Does my service survive random pod failures?"*

Enable `kubernetes` with `kill_pod`, set `max_targets: 1`, use `continuous` scheduler with 5-minute intervals. Enable `healthcheck` to verify the service recovers between rounds.

```json
{
  "scenarios": [
    {
      "name": "pod-failure-resilience",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "retries": 0,
      "steps": [
        {
          "name": "kubernetes",
          "condition": "always",
          "retries": 0
        }
      ]
    }
  ]
}
```

### Scenario 2: Progressive Degradation Test
> *"How does my system degrade under multi-layer stress?"*

Orchestrate progressive attacks: start with network chaos, then add HTTP floods, finally hit Kubernetes.

```json
{
  "scenarios": [
    {
      "name": "network-baseline",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "steps": [
        { "name": "network", "condition": "always" }
      ]
    },
    {
      "name": "add-http-load",
      "enabled": true,
      "order": 20,
      "parallel": false,
      "prerequisites": ["network-baseline"],
      "steps": [
        { "name": "http", "condition": "always", "retries": 1 }
      ]
    },
    {
      "name": "infrastructure-attack",
      "enabled": true,
      "order": 30,
      "parallel": false,
      "prerequisites": ["add-http-load"],
      "steps": [
        { "name": "kubernetes", "condition": "always" }
      ]
    }
  ]
}
```

### Scenario 3: Parallel Multi-Vector Attack
> *"Can my system handle simultaneous failures across layers?"*

Execute stress + network + HTTP floods in parallel for maximum impact.

```json
{
  "scenarios": [
    {
      "name": "multi-vector-chaos",
      "enabled": true,
      "order": 10,
      "parallel": true,
      "prerequisites": [],
      "retries": 1,
      "steps": [
        { "name": "stress", "condition": "always" },
        { "name": "network", "condition": "always" },
        { "name": "http", "condition": "always" },
        { "name": "kubernetes", "condition": "always" }
      ]
    }
  ]
}
```

### Scenario 4: Recovery Verification with Conditional Steps
> *"Does my system recover gracefully after failures?"*

Run chaos, then use conditional steps to verify recovery (only if chaos succeeded).

```json
{
  "scenarios": [
    {
      "name": "chaos-phase",
      "enabled": true,
      "order": 10,
      "parallel": true,
      "prerequisites": [],
      "steps": [
        { "name": "network", "condition": "always" },
        { "name": "stress", "condition": "always" }
      ]
    },
    {
      "name": "recovery-check",
      "enabled": true,
      "order": 20,
      "parallel": false,
      "prerequisites": ["chaos-phase"],
      "retries": 3,
      "steps": [
        {
          "name": "http",
          "condition": "on_success",
          "retries": 5
        }
      ]
    }
  ]
}
```

### Scenario 5: Message Queue Backpressure
> *"What happens when a queue gets flooded with garbage?"*

Enable `mq` with a high `message_count` and `random_payload: true`. Monitor your consumer's behavior, dead-letter rates, and memory usage.

### Scenario 6: Network Partition Simulation
> *"Can my microservices handle network degradation?"*

Enable `network` with `latency` (200ms + 50ms jitter) and `packet_loss` (10%). Set a `duration` to auto-rollback. Combine with `healthcheck` to measure recovery time.

### Scenario 7: Full Chaos Day
> *"Run everything on a schedule and report to Slack."*

Enable all modules, set scheduler to `cron` with `@every 30m`, enable `metrics` for Grafana dashboards, `report` for audit trail, and `notify` with your Slack webhook.

---

## License

MIT
