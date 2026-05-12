# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

`AGENTS.md` is the canonical agent guide for this repo and covers conventions in
more depth (Cobra wiring, logging, AddJob lifecycle, code style, safety
checks). Read it whenever the answer is not in this file.

## Commands

Go version is pinned in `go.mod` (currently 1.26).

- Run: `go run .` (root help) / `go run . list-updater -c config.yaml` (subcommand)
- Build: `go build -o bin/infra-helper .`
- Format: `gofmt -w .` (mandatory; lint enforces it)
- Lint: `golangci-lint run ./...` — config in `.golangci.yml` uses `default: all`
  with only `wsl`, `exhaustruct`, `gochecknoglobals`, `gochecknoinits` disabled,
  so most lint rules are on. `cyclop` max-complexity is 15, `funlen` max is 130.
- Vet only: `go vet ./...`
- Test: `go test ./...` (add `-race` for concurrency code).
  Single test: `go test ./pkg/app -run '^TestThing$' -count=1`

Common root flags (see `cmd/root.go`):

- `--debug` / `-v` — switches zerolog to console writer at debug level.
- `--version` — prints build info and exits.
- `--metrics <addr>` / `--no-metrics` — controls the Prometheus exporter.
  Default address `:9101`. Setting `--metrics ""` (empty value) is treated as
  `--no-metrics` in `PersistentPreRun`.

## Architecture

### Process lifecycle is owned by `pkg/app`

Every subcommand goes through the same lifecycle, set up in
`cmd/root.go`'s `PersistentPreRun` → `app.Init(debug)`:

1. `app.Init` builds a global cancellable `context.Background()`, installs a
   SIGINT/SIGTERM handler, and spawns a watcher goroutine that waits for either
   a signal or `app.Cancel()`, then waits up to 10s for `WG()` to drain before
   `os.Exit`.
2. If metrics aren't disabled, `cmd/root.go` starts `app.StartMetrics` as a job
   and calls `app.SetName(cmd.CommandPath())` so the `app{name=...}` gauge is
   labeled with the invoked subcommand path.
3. `PersistentPostRun` does `time.Sleep(1s)` then `app.WG().Wait()`. The 1s
   pause is intentional — it gives subcommand `Run` handlers a chance to call
   `app.AddJob` (which `WG.Add(1)`s) before `Wait` is reached. Don't remove it
   without rethinking the handshake.

### `app.AddJob` is the only correct way to start a long-running task

```go
ctx, onStop := app.AddJob("listUpdater.refresh")
defer onStop()  // mandatory exactly once
```

- `name` must be non-empty and unique across the process — `AddJob` calls
  `log.Fatal` on duplicates or empty names. Use a package-level const, and
  prefix with the subcommand name (e.g. `"listUpdater.http"`,
  `"listUpdater.refresh"`).
- `ctx` is derived from the global app context; cancel propagates from signals
  or `app.Cancel()`.
- The returned context carries the job name under `app.ContextKeyJobName{}`.
- Jobs registered into `Jobs` (`sync.Map`) and a `job{name=...}` gauge while
  alive.

### Adding a subcommand

1. Create `cmd/<name>/cmd.go` (package can be the same name; existing
   `listUpdater` uses package `listupdater`).
2. Export a `Register(parent *cobra.Command)` function that calls
   `parent.AddCommand(...)` — `cmd/root.go`'s `init()` calls it.
3. Keep the Cobra `Run` thin: `AddJob`, load config via `app.ReadFromFile`,
   validate, then call into a real package under `cmd/<name>/internal/...` (the
   convention `listUpdater` follows) or `pkg/...`.
4. Block on `<-ctx.Done()` before returning if you launched goroutines that
   must keep running.

### Config pattern (`app.ReadFromFile`)

- Define a struct with `mapstructure`, `yaml`, and `default` tags
  (`creasty/defaults`). See `cmd/listUpdater/internal/listupdater/types.go`.
- `ReadFromFile` writes a default YAML if the file doesn't exist, applies
  `defaults.Set`, then `viper.Unmarshal`s. `viper.AutomaticEnv()` is on.
- Validate after loading; the `list-updater` validator in `cmd.go` is the
  reference for required-field + duplicate-name checks.

### `list-updater` subcommand

Caching proxy for downloadable lists, defined in
`cmd/listUpdater/internal/listupdater/`:

- `service.go` is the entry point (`startListUpdater`); it spawns
  `runRefresher` (periodic sync via `s.cfg.Refresh` ticker) and `runHTTP`
  (Echo v5 server) — each is its own `AddJob`.
- Originals live in `<dir>/original/<name>`; plain text in
  `<dir>/plain/<name>`; per-category plains in
  `<dir>/plain/categories/<name>/<category>`.
- Routes (registration order matters because `/:name` is a catch-all):
  `/healthz`, `/plain/:name/`, `/plain/:name/:category`, `/plain/:name`, `/`,
  `/:name`. Don't reorder without checking the comment in `runHTTP`.
- `.dat` originals are protobuf `GeoSiteList` (v2ray-core/v5); plain
  conversion is in `plain.go`. Plain-text originals pass through unchanged.
- `isSafeSegment` whitelists `[A-Za-z0-9._-]` for any path param — keep new
  handlers using it; never `filepath.Join` raw user input.
- HTTP errors for category endpoints route through `mapCategoryPlainError`,
  which translates `errCategoryNotFound` → 404 and `errCategoryUnsupported`
  → 400. Use the same sentinel pattern when adding errors.

### Logging and metrics

- Default zerolog output is JSON. `--debug` switches to a console writer with
  caller info. Use structured fields (`Str`, `Dur`, `Int`...), not `Msgf`.
- Echo handlers should use `app.EchoZerologMiddleware()` (truncates URI to 2
  KiB and maps status to log level).
- `app.RegisterMetric(...)` registers into the global registry; the metrics
  HTTP server also serves `/healthz` and `/readyz` driven by
  `app.SetHealthy` / `app.SetReady`.

## Repo notes

- The committed `zapret.dat` (~28 MB) is sample data for `list-updater`; don't
  treat it as code.
- The README ends with personal `rsync` / `yc` snippets — they're scratch
  notes, not project commands.
