# YACMO Agents (Chaos Modules)

This document describes each chaos experiment module ("agent") that YACMO supports, how to configure them, and how to use them in scenarios.

---

## Overview

YACMO provides six independent chaos modules, each targeting a different layer of your infrastructure:

| Agent | ID | Purpose | Destructive | Module |
|---|---|---|---|---|
| **Kubernetes** | `kubernetes` | Pod/deployment/service chaos | ✓ Yes | `pkg/k8s/` |
| **HTTP** | `http` | Traffic injection, floods, latency | • Maybe | `pkg/httpflood/` |
| **gRPC** | `grpc` | RPC floods, metadata injection | • Maybe | `pkg/grpcflood/` |
| **Message Queue** | `mq` | Broker flooding (RabbitMQ, Kafka, NATS) | ✓ Yes | `pkg/mqflood/` |
| **Network** | `network` | Latency, packet loss, DNS failure | ✓ Yes | `pkg/network/` |
| **Resource Stress** | `stress` | CPU, memory, disk I/O burn | ✓ Yes | `pkg/stress/` |

---

## 1. Kubernetes Agent (`kubernetes`)

### Purpose
Inject chaos into Kubernetes infrastructure by killing pods, scaling down deployments, and deleting services.

### Configuration

```json
{
  "kubernetes": {
    "enabled": true,
    "kubeconfig": "",
    "namespaces": ["default", "production"],
    "label_selectors": ["app=myapp"],
    "actions": ["kill_pod", "scale_down"],
    "max_targets": 1,
    "grace_period_seconds": 30,
    "excluded_pods": ["critical-daemon", "monitoring"]
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `enabled` | bool | `false` | Enable/disable this agent |
| `kubeconfig` | string | `""` (in-cluster) | Path to kubeconfig file; empty uses in-cluster auth |
| `namespaces` | []string | `["default"]` | Target namespaces |
| `label_selectors` | []string | `[]` | Kubernetes label selectors (e.g., `app=web`) |
| `actions` | []string | `[]` | Actions to execute: `kill_pod`, `scale_down`, `delete_service` |
| `max_targets` | int | `1` | Max resources to target per round |
| `grace_period_seconds` | int64 | `30` | Grace period for pod termination |
| `excluded_pods` | []string | `[]` | Pod name patterns to never touch |

### Actions

#### `kill_pod`
Randomly selects eligible pods and deletes them with a configurable grace period.

- **Effect**: Pod is terminated and should be rescheduled by its controller
- **Rollback**: Pod deletion is final; relies on controller to reschedule
- **Safety**: Respects `excluded_pods` patterns, label selectors, and `max_targets`

#### `scale_down`
Scales selected deployments to 0 replicas.

- **Effect**: All pods in deployment are terminated
- **Rollback**: Original replica count is restored
- **Safety**: Tracked deployment state for accurate rollback

#### `delete_service`
Deletes selected services.

- **Effect**: Service is removed; endpoints are unavailable
- **Rollback**: ❌ Service deletion cannot be rolled back (rebuild via controller)
- **Safety**: Always protects the `kubernetes` service (built-in API service)

### Example Scenario

```json
{
  "scenarios": [
    {
      "name": "k8s-pod-chaos",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "retries": 1,
      "steps": [
        {
          "name": "kubernetes",
          "condition": "always"
        }
      ]
    }
  ]
}
```

### Use Cases

- **Pod crash simulation**: Test if your app recovers when pods die
- **Deployment replica loss**: See how your service handles reduced capacity
- **Service discovery failure**: Test fallback behavior when services disappear

---

## 2. HTTP Agent (`http`)

### Purpose
Inject HTTP traffic floods, latency, error injection, and payload corruption into HTTP services.

### Configuration

```json
{
  "http": {
    "enabled": true,
    "targets": [
      {
        "name": "api-flood",
        "url": "http://api.example.com/submit",
        "method": "POST",
        "headers": {
          "Content-Type": "application/json",
          "User-Agent": "YACMO/1.0"
        },
        "body": "{\"request\":\"data\"}",
        "concurrency": 50,
        "total_requests": 10000,
        "rate_per_second": 500.0,
        "timeout_seconds": 10,
        "duration": 60000000000,
        "inject_latency": 100000000,
        "inject_error_ratio": 0.05,
        "randomize_body": false,
        "body_size_bytes": 1024
      }
    ]
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | — | Unique target identifier |
| `url` | string | — | Target URL (required) |
| `method` | string | `GET` | HTTP method (GET, POST, PUT, DELETE, PATCH, HEAD) |
| `headers` | map | `{}` | Custom HTTP headers |
| `body` | string | `""` | Request body (for POST/PUT/PATCH) |
| `concurrency` | int | `10` | Number of concurrent workers |
| `total_requests` | int | `0` (unlimited) | Total requests to send |
| `rate_per_second` | float64 | `0` (unlimited) | Rate limit (requests/sec) |
| `timeout_seconds` | int | `30` | Per-request timeout |
| `duration` | duration | `30s` | How long to run the flood |
| `inject_latency` | duration | `0` | Artificial delay before each request |
| `inject_error_ratio` | float64 | `0.0` | Ratio of requests to abort [0.0-1.0] |
| `randomize_body` | bool | `false` | Generate random binary payloads instead of `body` |
| `body_size_bytes` | int | `0` | Size of random payload if `randomize_body: true` |

### Example Targets

**Simple GET Flood**
```json
{
  "name": "health-check-flood",
  "url": "http://localhost:8080/health",
  "method": "GET",
  "concurrency": 20,
  "rate_per_second": 100.0,
  "duration": 30000000000
}
```

**POST with Latency Injection**
```json
{
  "name": "api-with-slowness",
  "url": "http://api.example.com/data",
  "method": "POST",
  "body": "{\"event\":\"chaos\"}",
  "concurrency": 10,
  "rate_per_second": 50.0,
  "inject_latency": 500000000,
  "duration": 60000000000
}
```

**Chaotic Payload Generation**
```json
{
  "name": "garbage-flood",
  "url": "http://localhost:9000/upload",
  "method": "POST",
  "concurrency": 5,
  "randomize_body": true,
  "body_size_bytes": 4096,
  "duration": 30000000000
}
```

### Example Scenario

```json
{
  "scenarios": [
    {
      "name": "http-load-test",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "retries": 0,
      "steps": [
        {
          "name": "http",
          "condition": "always",
          "retries": 2
        }
      ]
    }
  ]
}
```

### Use Cases

- **Load testing**: Measure API capacity and breaking point
- **Latency tolerance**: Test if clients handle slow responses
- **Error handling**: Verify error recovery logic (with `inject_error_ratio`)
- **Payload resilience**: Test robustness against malformed data

---

## 3. gRPC Agent (`grpc`)

### Purpose
Inject gRPC traffic floods with concurrent RPC calls, metadata injection, and payload control.

### Configuration

```json
{
  "grpc": {
    "enabled": true,
    "targets": [
      {
        "name": "grpc-service-flood",
        "address": "grpc.example.com:50051",
        "method": "/myapp.Service/MyRPC",
        "insecure": true,
        "metadata": {
          "authorization": "Bearer token123",
          "user-id": "test-user"
        },
        "payload": "{\"request\":\"data\"}",
        "random_payload": false,
        "payload_size_bytes": 512,
        "concurrency": 20,
        "total_requests": 5000,
        "rate_per_second": 100.0,
        "duration": 30000000000
      }
    ]
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | — | Unique target identifier |
| `address` | string | — | gRPC server address (host:port) |
| `method` | string | — | Full method path (e.g., `/package.Service/Method`) |
| `insecure` | bool | `false` | Use plaintext (no TLS) |
| `metadata` | map | `{}` | gRPC metadata (headers) |
| `payload` | string | `""` | Static payload (JSON or binary) |
| `random_payload` | bool | `false` | Generate random payloads |
| `payload_size_bytes` | int | `0` | Size of random payload |
| `concurrency` | int | `10` | Concurrent connections |
| `total_requests` | int | `0` (unlimited) | Total RPC calls to send |
| `rate_per_second` | float64 | `0` (unlimited) | Rate limit (calls/sec) |
| `duration` | duration | `30s` | How long to run |

### Example Scenario

```json
{
  "scenarios": [
    {
      "name": "grpc-stress",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "steps": [
        {
          "name": "grpc",
          "condition": "always",
          "retries": 1
        }
      ]
    }
  ]
}
```

### Use Cases

- **RPC load testing**: Benchmark gRPC services
- **Concurrent connection stress**: Test connection pool limits
- **Metadata handling**: Verify correct handling of gRPC headers

---

## 4. Message Queue Agent (`mq`)

### Purpose
Flood message brokers (RabbitMQ, Kafka, NATS) with messages to test consumer backpressure and recovery.

### Configuration

```json
{
  "mq": {
    "enabled": true,
    "backends": [
      {
        "name": "rabbitmq-flood",
        "type": "rabbitmq",
        "broker_url": "amqp://guest:guest@localhost:5672/",
        "topic": "chaos-queue",
        "queue": "chaos-queue",
        "username": "guest",
        "password": "guest",
        "message_count": 10000,
        "message_size": 1024,
        "concurrency": 10,
        "duration": 60000000000,
        "rate_per_sec": 500.0,
        "random_payload": true,
        "payload_pattern": "{\"event\":\"chaos\",\"index\":%d}"
      },
      {
        "name": "kafka-flood",
        "type": "kafka",
        "broker_url": "localhost:9092",
        "topic": "chaos-topic",
        "message_count": 50000,
        "message_size": 512,
        "concurrency": 20,
        "rate_per_sec": 1000.0,
        "random_payload": false,
        "payload_pattern": "chaos-message-%d"
      },
      {
        "name": "nats-flood",
        "type": "nats",
        "broker_url": "nats://localhost:4222",
        "topic": "chaos.subject",
        "message_count": 100000,
        "message_size": 256,
        "concurrency": 50,
        "rate_per_sec": 5000.0,
        "random_payload": true
      }
    ]
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `name` | string | — | Unique backend identifier |
| `type` | string | — | Broker type: `rabbitmq`, `kafka`, `nats` |
| `broker_url` | string | — | Connection URL (required) |
| `topic` | string | — | Topic/queue name |
| `queue` | string | — | Queue name (RabbitMQ only) |
| `username` | string | `""` | Authentication username |
| `password` | string | `""` | Authentication password |
| `message_count` | int | `1000` | Total messages to send |
| `message_size` | int | `1024` | Message size in bytes |
| `concurrency` | int | `10` | Concurrent producers |
| `duration` | duration | `30s` | How long to run |
| `rate_per_sec` | float64 | `0` (unlimited) | Rate limit (messages/sec) |
| `random_payload` | bool | `false` | Generate random payloads |
| `payload_pattern` | string | `""` | Template for message content |

### Example Scenario

```json
{
  "scenarios": [
    {
      "name": "queue-backpressure",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "steps": [
        {
          "name": "mq",
          "condition": "always",
          "retries": 0
        }
      ]
    }
  ]
}
```

### Use Cases

- **Consumer backpressure**: Test behavior when queues fill up
- **Message processing speed**: Measure throughput under stress
- **Dead-letter queue handling**: Monitor failed message handling

---

## 5. Network Agent (`network`)

### Purpose
Inject network-level chaos: latency, packet loss, DNS failure, bandwidth limits, and packet corruption.

### Configuration

```json
{
  "network": {
    "enabled": true,
    "interface": "eth0",
    "actions": ["latency", "packet_loss"],
    "latency_ms": 200,
    "jitter_ms": 50,
    "packet_loss_percent": 10.0,
    "corrupt_percent": 0.0,
    "bandwidth_limit_kbps": 1000,
    "duration": 300000000000
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `interface` | string | `eth0` | Network interface (e.g., `eth0`, `en0`) |
| `actions` | []string | `[]` | Actions: `latency`, `packet_loss`, `dns_failure`, `bandwidth_limit`, `corrupt` |
| `latency_ms` | int | `0` | Latency to add (milliseconds) |
| `jitter_ms` | int | `0` | Jitter variance (milliseconds) |
| `packet_loss_percent` | float64 | `0.0` | Packet loss percentage [0.0-100.0] |
| `corrupt_percent` | float64 | `0.0` | Packet corruption percentage [0.0-100.0] |
| `bandwidth_limit_kbps` | int | `0` | Bandwidth limit (kilobits/sec) |
| `duration` | duration | `30s` | How long to apply chaos |

### Actions

| Action | Tool | Parameters | Rollback |
|---|---|---|---|
| `latency` | `tc netem` | `latency_ms`, `jitter_ms` | Auto (via duration) |
| `packet_loss` | `tc netem` | `packet_loss_percent` | Auto (via duration) |
| `dns_failure` | `iptables` | (blocks UDP/TCP 53) | Auto (via duration) |
| `bandwidth_limit` | `tc tbf` | `bandwidth_limit_kbps` | Auto (via duration) |
| `corrupt` | `tc netem` | `corrupt_percent` | Auto (via duration) |

### Requirements

- Linux OS with `tc` (traffic control) and `iptables` available
- Root/sudo privileges to configure network rules
- Single interface per chaos run

### Example Scenario

```json
{
  "scenarios": [
    {
      "name": "network-degradation",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "steps": [
        {
          "name": "network",
          "condition": "always"
        }
      ]
    }
  ]
}
```

### Use Cases

- **Latency tolerance**: Test client/server behavior under network delay
- **Packet loss resilience**: Verify TCP/UDP retry logic
- **DNS failure**: Test fallback when DNS is unavailable
- **Bandwidth limiting**: Simulate slow networks (3G, satellite)

---

## 6. Stress Agent (`stress`)

### Purpose
Burn CPU, memory, and disk I/O to test resource pressure handling.

### Configuration

```json
{
  "stress": {
    "enabled": true,
    "actions": ["cpu", "memory", "disk_io", "disk_fill"],
    "duration": 60000000000,
    "cpu_cores": 2,
    "memory_mb": 512,
    "disk_io_workers": 4,
    "temp_dir": "/tmp",
    "disk_fill_mb": 1024
  }
}
```

| Field | Type | Default | Description |
|---|---|---|---|
| `actions` | []string | `[]` | Actions: `cpu`, `memory`, `disk_io`, `disk_fill` |
| `duration` | duration | `30s` | How long to stress |
| `cpu_cores` | int | `0` (all) | Number of CPU cores to burn |
| `memory_mb` | int | `0` (skip) | Memory to allocate (MB) |
| `disk_io_workers` | int | `1` | Parallel I/O workers |
| `temp_dir` | string | `/tmp` | Directory for temp files |
| `disk_fill_mb` | int | `0` (skip) | Disk fill size (MB) |

### Actions

| Action | Effect | Rollback |
|---|---|---|
| `cpu` | Tight CPU loops on N cores | Goroutines exit after duration |
| `memory` | Allocates N MB (touches every page) | Memory freed after duration |
| `disk_io` | Parallel random read/write | Temp files cleaned up |
| `disk_fill` | Creates large temp file | Temp file deleted |

### Example Scenario

```json
{
  "scenarios": [
    {
      "name": "resource-exhaustion",
      "enabled": true,
      "order": 10,
      "parallel": false,
      "prerequisites": [],
      "steps": [
        {
          "name": "stress",
          "condition": "always"
        }
      ]
    }
  ]
}
```

### Use Cases

- **CPU scaling**: Test autoscaler response to CPU pressure
- **Memory limits**: Verify OOM killer behavior
- **Disk I/O**: Test application behavior under disk contention
- **Storage saturation**: See how services handle full disks

---

## Combining Agents in Scenarios

### Serial Execution (Dependency Chain)

```json
{
  "scenarios": [
    {
      "name": "phase-1-network",
      "order": 10,
      "parallel": false,
      "steps": [{"name": "network"}]
    },
    {
      "name": "phase-2-http-load",
      "order": 20,
      "prerequisites": ["phase-1-network"],
      "parallel": false,
      "steps": [{"name": "http"}]
    }
  ]
}
```

### Parallel Execution (Multi-Vector Attack)

```json
{
  "scenarios": [
    {
      "name": "multi-vector",
      "order": 10,
      "parallel": true,
      "steps": [
        {"name": "network"},
        {"name": "http"},
        {"name": "stress"},
        {"name": "kubernetes"}
      ]
    }
  ]
}
```

### Conditional Recovery Verification

```json
{
  "scenarios": [
    {
      "name": "chaos",
      "order": 10,
      "parallel": true,
      "steps": [
        {"name": "network"},
        {"name": "stress"}
      ]
    },
    {
      "name": "recovery-check",
      "order": 20,
      "prerequisites": ["chaos"],
      "parallel": false,
      "retries": 3,
      "steps": [
        {"name": "http", "condition": "on_success", "retries": 5}
      ]
    }
  ]
}
```

---

## Agent Execution Order in Scenarios

1. **Scenarios are sorted by `order`** (lower values run first)
2. **Within a scenario**, steps run either:
   - **Sequential** if `parallel: false` (default)
   - **Concurrent** if `parallel: true`
3. **Conditional steps** (`on_success`, `on_failure`) only run if their condition is met
4. **Prerequisite scenarios** are checked before a scenario runs
5. **Retries** are applied per-step (with per-step override) and can be configured at scenario level

---

## Safety Considerations per Agent

| Agent | Destructive | Requires Approval | Rollback Type | Risk Level |
|---|---|---|---|---|
| `kubernetes` | ✓ | Interactive confirm | Tracked state | 🔴 High |
| `http` | • | No | N/A (load test) | 🟡 Medium |
| `grpc` | • | No | N/A (load test) | 🟡 Medium |
| `mq` | ✓ | Interactive confirm | Duration-based | 🟡 Medium |
| `network` | ✓ | Interactive confirm | Duration-based + cleanup | 🔴 High |
| `stress` | ✓ | Interactive confirm | Duration-based + cleanup | 🔴 High |

**Note**: Destructive agents require `safety.allow_destructive_actions: true` and may require interactive confirmation.

---

## References

- [README.md](README.md) — Main documentation
- [config_examples/](config_examples/) — Configuration examples
- [pkg/chaos/engine.go](pkg/chaos/engine.go) — Scenario orchestration engine
- [pkg/config/config.go](pkg/config/config.go) — Configuration types


