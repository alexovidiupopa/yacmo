# CLAUDE.md

Guidance for working in this repository. For the user-facing description of each chaos
module and its config, see [AGENTS.md](AGENTS.md) and [README.md](README.md).

## What this is

YACMO ("Yet Another Chaos Monkey") is a single-binary chaos-engineering tool written in
Go. It injects controlled failures across six layers — Kubernetes, HTTP, gRPC, message
queues, network, and system resources — driven by one JSON config file. It runs as a CLI,
not a server (though it can expose a Prometheus metrics endpoint while running).

- Module path: `yacmo` (Go 1.25). All internal imports are `yacmo/pkg/...`.
- Entry point: [main.go](main.go).

## Commands

```bash
go build -o yacmo .        # build the binary
go test -v ./...           # run tests (CI runs this; see note below)
go vet ./...               # static checks
./yacmo -config config_examples/config-dry-run.json   # run in dry-run
```

Common flags: `-config <path>` (default `config.json`), `-dry-run`, `-log-level
debug|info|warn|error`, `-approve` (approve destructive actions), `-version`.

CI/CD: [.github/workflows/go.yml](.github/workflows/go.yml) runs on push/PR to `master`,
on `release/**` branches, and on `v*` tags. It cross-compiles versioned binaries
(uploaded as artifacts) and runs the tests via `gotestsum`, publishing JUnit + coverage
as artifacts and a coverage summary to the run's job summary. The build version is stamped
into the binary via `-ldflags` into [pkg/version](pkg/version/version.go), so
`yacmo -version` reflects the artefact. Pushing `release/X.Y.Z` produces an
`X.Y.Z-rc.<run>` pre-release; pushing tag `vX.Y.Z` publishes a full GitHub Release with
the binaries attached.

> **Tests:** `config`, `safety`, `chaos`, `report`, `healthcheck`, `httpflood`, and
> `logger` have unit tests (no external infra needed — HTTP paths use `httptest`). The
> infra-bound modules (`k8s`, `network`, `stress`, `mqflood`, `grpcflood`) are not yet
> covered. When adding functionality, add tests — CI runs `go test -v ./...`.

## Architecture

Flow: `main.go` loads/validates config → runs a safety preflight → wires cross-cutting
concerns (metrics, notify, report, healthcheck) → registers enabled experiments on the
engine → a `Scheduler` drives the `Engine`, which runs experiments (or scenarios) →
health-check comparison, summary, report write, best-effort rollback.

```
main.go → scheduler → chaos.Engine → experiments (k8s/http/grpc/mq/network/stress)
                                    ↘ callbacks → metrics / report / notify
```

### Key packages (`pkg/`)

- `config/` — all config structs, `LoadFromFile`, `DefaultConfig`, `Validate`. Config is
  JSON; `time.Duration` fields are **nanoseconds as integers** in JSON (e.g. `30s` = `30000000000`).
- `chaos/` — the `Engine` and the central `Experiment` interface. This is the hub.
- `safety/` — `Policy` guardrails: namespace/name allow+block lists, destructive-action
  caps, approval/fail-closed logic, binary preflight (`tc`, `iptables`).
- `scheduler/` — `once` / `continuous` / `cron` run modes.
- `k8s/`, `httpflood/`, `grpcflood/`, `mqflood/`, `network/`, `stress/` — the six chaos
  modules. `mqflood/` has per-backend producers (amqp/kafka/nats).
- `report/` — builds JSON / HTML / CSV reports (see `report_examples/`).
- `metrics/`, `notify/`, `healthcheck/`, `logger/` — cross-cutting support.

### The `Experiment` interface — the extension point

Defined in [pkg/chaos/engine.go](pkg/chaos/engine.go):

```go
type Experiment interface {
    Name() string
    Run(ctx context.Context) error       // must respect ctx cancellation
    Rollback(ctx context.Context) error  // best-effort undo
}
```

Destructive modules also implement `DestructiveActionCount() int` (checked reflectively
via a `destructiveReporter` interface) so the engine can enforce safety caps.

To **add a new chaos module**:
1. Create `pkg/<module>/` with a type implementing `Experiment` (and
   `DestructiveActionCount` if destructive).
2. Add its config struct + `Validate` rules in `pkg/config/config.go`.
3. Register it in `main.go` with `engine.RegisterNamed("<id>", ...)` behind
   `if cfg.<Module>.Enabled`. The `<id>` is what scenarios reference by `name`.
4. If destructive, add safety wiring in `pkg/safety/safety.go`
   (`isDestructiveAction`, action checks).
5. Document it in AGENTS.md + README.md and add a `config_examples/` file.

## Conventions

- **Safety first.** Destructive actions (k8s kill/scale/delete, network, stress) are
  blocked unless `safety.allow_destructive_actions: true`, and gated by approval
  (`-approve` flag or interactive confirm). `DefaultConfig()` ships with `dry_run: true`
  and safety fully enabled — keep it that way. Dry-run skips all real execution.
- **Context propagation.** Every `Run`/`Rollback` takes a `context.Context` and must honor
  cancellation (SIGINT/SIGTERM trigger `cancel()` in main).
- **Logging** goes through `pkg/logger` (`log.Info/Warn/Error/Debug` with printf-style
  args) — not `fmt.Println`/stdlib `log` directly. It uses emoji/box-drawing prefixes for
  summaries; match the existing style.
- **Config validation** lives in `Config.Validate()` — add per-module checks there, and
  set sane defaults in `DefaultConfig()`.
- Standard Go: `gofmt`, wrap errors with `fmt.Errorf("...: %w", err)`, table-free small
  helpers. Keep new code matching the surrounding file's idiom.

## Notable details / gotchas

- The committed `yacmo` binary (~70MB) is in `.gitignore` along with `.idea` and
  `reports/`. Don't commit build artifacts or generated reports.
- Network module requires Linux with `tc` + `iptables` and root; it will fail preflight
  elsewhere (e.g. macOS dev machines). Use dry-run to exercise other paths locally.
- Scenarios (config `scenarios[]`) override the simple sequential run: ordered by `order`,
  support `parallel`, `prerequisites`, per-step/scenario `retries`, and conditions
  (`always`/`on_success`/`on_failure`). Logic is in `Engine.runScenarios`.
- Report format is chosen by `report.format` (`json`|`html`|`csv`); examples live in
  `report_examples/`.
