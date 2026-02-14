# infra-helper

`infra-helper` - CLI-инструмент (Cobra) для инфраструктурных задач.

## Общая архитектура

- Команды: `infra-helper/cmd/...`.
- Общая основа для подприложений: `infra-helper/pkg/app`.
  - логирование: `zerolog`
  - управление жизненным циклом: `app.Init(...)`, `app.Cancel()`
  - фоновые джобы: `app.AddJob(name)` -> `(ctx, onStop)` (обязательно `defer onStop()`)
  - метрики: Prometheus (в `infra-helper/pkg/app/metrics.go`)

## Команды

### list-updater

Сервис, который по YAML-конфигу скачивает списки по URL, кеширует оригиналы и генерирует plain-версию.

- HTTP сервер: `github.com/labstack/echo/v5`
- Описание/конфиг: `infra-helper/cmd/listUpdater/README.md`

## Разработка

Запуск:

- `go run .`
- `go run . list-updater`

Проверки:

- `gofmt -w .`
- `golangci-lint run ./...`
- `go test ./...`
