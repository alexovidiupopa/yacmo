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
- [Experiment Modules](#experiment-modules)
- [Observability](#observability)
- [Safety Mechanisms](#safety-mechanisms)
- [Scenarios](#scenarios)

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
          ┌────────────────▼────────────────┐
          │        Scheduler                │
          │  once · continuous · cron       │
          └────────────────┬────────────────┘
                           │
          ┌────────────────▼────────────────┐
          │        Chaos Engine             │
          │  sequential execution           │
          │  result callbacks               │
          │  rollback orchestration         │
          └──┬──┬──┬──┬──┬──┬──────────────┘
             │  │  │  │  │  │
    ┌────────┘  │  │  │  │  └────────┐
    ▼           ▼  ▼  ▼  ▼           ▼
┌──────┐  ┌────┐┌────┐┌──┐  ┌───────┐┌──────┐
│ K8s  │  │HTTP││gRPC││MQ│  │Network││Stress│
└──────┘  └────┘└────┘└──┘  └───────┘└──────┘

    ┌──────────────────────────────────────┐
    │         Cross-cutting concerns       │
    │  Metrics · Reports · Notifications   │
    │  Health Checks · Logging             │
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
│   ├── report/report.go           # JSON report generation
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

- Generates timestamped JSON reports after each run
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
| `cron` | Supports `@every 5m` and `*/N * * * *` expressions |

---

## Getting Started

### Build

```bash
go build -o yacmo .
```

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

---

## Scenarios

### Scenario 1: Kubernetes Resilience Test
> *"Does my service survive random pod failures?"*

Enable `kubernetes` with `kill_pod`, set `max_targets: 1`, use `continuous` scheduler with 5-minute intervals. Enable `healthcheck` to verify the service recovers between rounds.

### Scenario 2: API Load + Failure Simulation
> *"How does my API behave under load while infrastructure is degraded?"*

Enable `http` flood against your API and `kubernetes` kill_pod simultaneously. The engine runs them sequentially — first the K8s chaos, then the HTTP flood hits the degraded system.

### Scenario 3: Message Queue Backpressure
> *"What happens when a queue gets flooded with garbage?"*

Enable `mq` with a high `message_count` and `random_payload: true`. Monitor your consumer's behavior, dead-letter rates, and memory usage.

### Scenario 4: Network Partition Simulation
> *"Can my microservices handle network degradation?"*

Enable `network` with `latency` (200ms + 50ms jitter) and `packet_loss` (10%). Set a `duration` to auto-rollback. Combine with `healthcheck` to measure recovery time.

### Scenario 5: Resource Exhaustion
> *"Does my system degrade gracefully under resource pressure?"*

Enable `stress` with `cpu` (2 cores) and `memory` (512MB) for 60 seconds. Monitor your application's response times and error rates.

### Scenario 6: Full Chaos Day
> *"Run everything on a schedule and report to Slack."*

Enable all modules, set scheduler to `cron` with `@every 30m`, enable `metrics` for Grafana dashboards, `report` for audit trail, and `notify` with your Slack webhook.

---

## License

MIT

