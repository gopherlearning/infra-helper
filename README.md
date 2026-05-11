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

### dns

Рекурсивный DNS-сервер с **fakeip** для заблокированных доменов и проксированием
остальных запросов на upstream-резолверы (UDP / TCP / DoT / DoH).

- Источники правил — `.dat` файлы формата v2ray/xray geosite, обновляются по `update_interval`.
- Для совпадающих доменов выдаётся **детерминированный** IP из заданного диапазона
  (`fakeip` action), либо `NXDOMAIN` (`block`), либо запрос идёт в upstream (`direct`).
- Приоритет действий: `block` > `fakeip` > `direct` > upstream.
- Параллельный запрос ко всем upstream'ам, возвращается первый успешный ответ.
- HTTP admin-сервер (по умолчанию `:8080`): `GET /healthz`, `GET /metrics`, `POST /reload`.

Пример конфига: `infra-helper/cmd/dns/config.yaml.example`.

#### Запуск локально

```sh
go run . dns -c cmd/dns/config.yaml.example
```

Для прослушивания на привилегированном порту 53 — либо `sudo`, либо
`setcap 'cap_net_bind_service=+ep' ./infra-helper`.

#### Запуск из готового образа (ghcr)

Образ собирается GitHub Actions и публикуется в `ghcr.io/<owner>/<repo>`.
Подставьте свои значения вместо `<owner>/<repo>`:

```sh
docker pull ghcr.io/<owner>/<repo>:latest

# host networking — нужно, чтобы DNS-сервис видел реальные клиенты.
# cap_add NET_BIND_SERVICE — чтобы слушать :53 без root.
docker run -d \
  --name infra-helper-dns \
  --restart unless-stopped \
  --network host \
  --cap-add NET_BIND_SERVICE \
  -v $(pwd)/config.yaml:/etc/infra-helper/config.yaml:ro \
  -v $(pwd)/cache:/var/cache/infra-helper \
  ghcr.io/<owner>/<repo>:latest \
  dns -c /etc/infra-helper/config.yaml --no-metrics
```

Минимальный `config.yaml`:

```yaml
server:
  listen: "0.0.0.0:53"
  protocols: [udp, tcp]
admin:
  listen: ":8080"
fakeip:
  ipv4_range: "198.18.0.0/15"
  ttl: 1
upstreams:
  - address: "tls://1.1.1.1:853"
  - address: "tls://77.88.8.8:853"
rulesets:
  - url: "https://example.com/zapret.dat"
    update_interval: "24h"
    tags:
      - name: "zapret"
        action: fakeip
cache:
  enabled: true
  size: 8192
cache_dir: "/var/cache/infra-helper"
```

#### docker-compose

```yaml
services:
  infra-helper-dns:
    image: ghcr.io/<owner>/<repo>:latest
    container_name: infra-helper-dns
    restart: unless-stopped
    network_mode: host
    cap_add:
      - NET_BIND_SERVICE
    volumes:
      - ./config.yaml:/etc/infra-helper/config.yaml:ro
      - ./cache:/var/cache/infra-helper
    command: ["dns", "-c", "/etc/infra-helper/config.yaml", "--no-metrics"]
```

#### Проверка

```sh
# обычный домен — идёт через upstream
dig @127.0.0.1 example.com

# домен из правил — должен вернуть IP из 198.18.0.0/15
dig @127.0.0.1 instagram.com

# health / metrics / reload
curl http://127.0.0.1:8080/healthz
curl http://127.0.0.1:8080/metrics | grep ^dns_
curl -X POST http://127.0.0.1:8080/reload
```

## CI / сборка образа

- `.github/workflows/build.yml` — GitHub Actions: `go test -race`, multi-arch
  сборка (`linux/amd64`, `linux/arm64`) и push в `ghcr.io/<owner>/<repo>`.
  Используется `GITHUB_TOKEN` (нужны права `packages: write` в репозитории).
- `.gitlab-ci.yml` — параллельная сборка через kaniko в GitLab Container Registry.

## Разработка

Запуск:

- `go run .`
- `go run . list-updater`
- `go run . dns`

Проверки:

- `gofmt -w .`
- `golangci-lint run ./...`
- `go test ./...`
