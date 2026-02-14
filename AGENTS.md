# infra-helper Agent Guide

This file is the project-local override for agents working in `infra-helper/`. It extends the baseline rules from the repository root `AGENTS.md`.

## Repo-Specific Rules Files

- Cursor rules: none found (`infra-helper/.cursor/rules/`, `infra-helper/.cursorrules`).
- Copilot rules: none found (`infra-helper/.github/copilot-instructions.md`).

## Project Overview

- CLI tool built on Cobra (`github.com/spf13/cobra`).
- Subcommands live under `infra-helper/cmd/...`.
- Shared runtime for subapps lives in `infra-helper/pkg/app` (logging, metrics, shutdown, job lifecycle).
- Logging backend: `zerolog` (keep it).

## Build / Run / Lint / Test

Go version: see `infra-helper/go.mod`.

- Run locally: `go run .`
- Run a subcommand: `go run . <subcommand> [flags]`
- Build binary: `go build -o bin/infra-helper .`

Formatting:

- Format: `gofmt -w .`
- Imports: `goimports -w .` (if installed)

Lint (use what the project has available):

- Minimal: `go vet ./...`
- If installed: `golangci-lint run ./...`

Tests:

- All: `go test ./...`
- With race: `go test -race ./...`

Run a single test:

- By name: `go test ./... -run '^TestThing$' -count=1`
- Subtest: `go test ./... -run '^TestThing$/^case name$' -count=1`
- Single package: `go test ./pkg/app -run '^TestThing$' -count=1`

## Cobra Conventions

- Root command: `infra-helper/cmd/root.go`.
- Add a subcommand by creating `infra-helper/cmd/<name>/cmd.go` and registering it from `infra-helper/cmd/root.go`.
- Keep Cobra `Run` handlers thin: parse flags/args, then call package-level logic.
- Prefer putting domain logic in `infra-helper/pkg/...` (not in `cmd/`).

Common flags live on the root command (`infra-helper/cmd/root.go`):

- `--debug` / `-v`: enable debug logging (console output).
- `--version`: print build info and exit.
- `--metrics` / `--no-metrics`: metrics export toggle (wiring may be TODO).

## Runtime, Logging, Metrics (pkg/app)

### Configuration (Viper + defaults)

- Config loader: `infra-helper/pkg/app/app.go` (`app.ReadFromFile`).
- Pattern: define a config struct, call `defaults.Set(&cfg)`, then `viper.Unmarshal(&cfg)`.
- Keep config I/O at the boundary (cmd or dedicated package); pass typed config into business logic.

### Logging (zerolog)

- Default output: JSON (good for piping and aggregators).
- Debug (`--debug` / `-v`): console writer with caller + debug level (human-readable).
- Prefer structured fields over formatting:
  - Prefer: `log.Info().Str("job", name).Msg("started")`
  - Avoid: `Msgf` for anything that could be fields.
- Never log secrets (tokens, passwords, full auth headers).

### Metrics (Prometheus)

- Metrics registry and collectors live in `infra-helper/pkg/app/metrics.go`.
- Register custom collectors via `app.RegisterMetric(...)`.
- If you wire metrics export, ensure shutdown is tied to the global app context.

Metrics HTTP handler also serves health endpoints:

- `GET /healthz` (healthy?)
- `GET /readyz` (ready?)

## Background Jobs Lifecycle (app.AddJob)

`app.AddJob(name)` is the standard lifecycle wrapper for long-running tasks.

Contract:

- It increments the global waitgroup and registers the job name.
- It returns `(ctx, onStop)`.
- You MUST call `defer onStop()` exactly once in the owning goroutine.
- `name` must be non-empty and unique; duplicates panic.

Naming guidance:

- Use a package-level `const jobName = "..."` to avoid typos.
- Include the subcommand name in the job name (e.g. `"listUpdater"`, `"metrics"`).

Canonical pattern:

```go
ctx, onStop := app.AddJob("listUpdater")
defer onStop()

for {
  select {
  case <-ctx.Done():
    return
  case <-ticker.C:
    // do work
  }
}
```

If you spawn extra goroutines inside a job:

- Tie them to `ctx.Done()` and ensure they exit before the owning function returns, OR
- Track them with `app.WG()` (`Add(1)` / `Done()`) if they must outlive the parent scope.

Shutdown:

- `app.Init(debug)` sets up global cancellation and a signal handler.
- To request a clean shutdown from code, call `app.Cancel()`.

## Go Code Style (Project-Specific)

- Formatting: `gofmt` is mandatory.
- Imports: stdlib, third-party, then internal (`infra.helper/...`).
- Naming: exported `MixedCaps`, unexported `mixedCaps`; avoid stutter (`app.AppApp`).
- Errors: wrap with `%w` when adding context; use `errors.Is/As`; avoid string matching.
- Context:
  - `ctx` is the first parameter when present.
  - Do not store `context.Context` in structs.
- Concurrency:
  - No goroutine leaks: every goroutine must have an exit path (usually `ctx.Done()`).
  - Avoid `time.Sleep` in production code for coordination; use timers/tickers/channels.

## Safety Checks Before You Finish

- Did every `AddJob` owner `defer onStop()`?
- Do all long-running loops select on `<-ctx.Done()`?
- Did you avoid logging secrets?
- Did you run `go test ./...` after lifecycle/shutdown changes?
