Вот ТЗ для Claude Code:

---

# ТЗ: DNS-сервер с fakeip для заблокированных доменов

## Обзор

Написать DNS-сервер на Go, который:
- Загружает и периодически обновляет списки доменов в формате v2ray/xray `.dat`
- Для доменов из указанных тегов возвращает фейковый IP из заданного диапазона
- Для остального трафика проксирует запросы на настраиваемые upstream DNS

---

## Конфигурация

Файл `config.yaml`:

```yaml
server:
  listen: "0.0.0.0:53"
  protocols: [udp, tcp]   # какие протоколы слушать

fakeip:
  ipv4_range: "198.18.0.0/15"   # диапазон фейковых IPv4
  ipv6_range: "fc00::/7"        # диапазон фейковых IPv6 (опционально)
  ttl: 1                         # TTL фейкового ответа в секундах

upstreams:
  - address: "tls://1.1.1.1:853"
  - address: "tls://77.88.8.8:853"
  - address: "https://8.8.8.8/dns-query"
  - address: "udp://77.88.8.8:53"

rulesets:
  - url: "https://github.com/kutovoys/ru_gov_zapret/releases/download/20260511034407/zapret.dat"
    update_interval: "24h"
    tags:                          # теги внутри .dat файла
      - name: "zapret"
        action: fakeip             # fakeip | block | direct
      - name: "zapret-zapad"
        action: fakeip
  - url: "https://example.com/another.dat"
    update_interval: "12h"
    tags:
      - name: "ads"
        action: block              # block = NXDOMAIN

cache:
  enabled: true
  size: 8192                       # количество записей
  ttl_override: false              # если true — переопределить TTL из ответа

log:
  level: "info"                    # debug | info | warn | error
  format: "text"                   # text | json
```

---

## Требования к реализации

### Загрузка и парсинг .dat файлов

- Формат `.dat` — protobuf, стандартный формат v2ray geosite (`github.com/v2fly/v2ray-core/app/router/routercommon`)
- При старте загружать все источники параллельно
- Хранить загруженные файлы в кэш-директории (`./cache/` или задаётся в конфиге) чтобы при рестарте не скачивать заново
- Обновлять по `update_interval` через ticker в фоновой горутине
- При ошибке обновления — логировать и продолжать работать со старыми данными
- После успешного обновления — атомарно заменять набор правил (RWMutex) без перезапуска

### Поиск домена в правилах

- Поддержать все типы матчинга из geosite: `domain` (suffix), `full`, `keyword`, `regexp`
- Порядок приоритетов: `block` > `fakeip` > `direct` > upstream
- Поиск должен быть эффективным — использовать trie или hashmap для suffix-матчинга

### fakeip логика

- Для каждого домена генерировать **детерминированный** фейковый IP из заданного диапазона (hash от FQDN → offset в диапазоне)
- Один и тот же домен всегда получает один и тот же фейковый IP в рамках текущей сессии
- На запросы типа `A` — возвращать IPv4 из `ipv4_range`
- На запросы типа `AAAA` — возвращать IPv6 из `ipv6_range` или `NOERROR` с пустым ответом если `ipv6_range` не задан
- TTL фейкового ответа берётся из конфига (`fakeip.ttl`)

### Upstream DNS

Поддержать протоколы:
- `udp://host:port` — обычный DNS по UDP
- `tcp://host:port` — DNS по TCP
- `tls://host:port` — DNS-over-TLS (DoT)
- `https://host/path` — DNS-over-HTTPS (DoH)

Логика выбора upstream:
- Параллельный запрос ко всем upstream, возвращать первый успешный ответ
- Таймаут на один upstream: 5 секунд (настраиваемо)
- При ошибке всех upstream — вернуть `SERVFAIL`

### Кэш DNS ответов

- Кэшировать ответы upstream (не fakeip — у них TTL=1)
- Уважать TTL из ответа при вытеснении
- Потокобезопасный доступ

### Операционные требования

- Graceful shutdown по SIGTERM/SIGINT — дождаться обработки текущих запросов
- HTTP endpoint для метрик и управления на отдельном порту (по умолчанию `8080`):
  - `GET /healthz` — liveness probe
  - `GET /metrics` — prometheus-совместимые метрики: запросы всего, fakeip хиты, upstream латентность, ошибки
  - `POST /reload` — принудительное обновление всех ruleset
- Логировать при старте: сколько доменов загружено из каждого тега каждого источника

---

## Структура проекта

```
.
├── main.go
├── config/
│   └── config.go          # парсинг yaml конфига
├── server/
│   └── server.go          # DNS listener (UDP+TCP)
├── resolver/
│   └── resolver.go        # логика обработки запроса
├── fakeip/
│   └── fakeip.go          # генерация детерминированных IP
├── ruleset/
│   ├── loader.go          # загрузка и обновление .dat
│   └── matcher.go         # trie/hashmap матчинг доменов
├── upstream/
│   └── upstream.go        # UDP/TCP/DoT/DoH клиенты
├── cache/
│   └── cache.go           # DNS кэш
├── metrics/
│   └── metrics.go         # prometheus метрики + HTTP сервер
├── Dockerfile
├── docker-compose.yml
├── config.yaml.example
└── README.md
```

---

## Dockerfile

Многоэтапная сборка, финальный образ на `scratch` или `alpine:3.19`. Бинарник статически слинкован. Образ должен запускаться от непривилегированного пользователя (кроме порта 53 — использовать `CAP_NET_BIND_SERVICE`).

---

## docker-compose.yml

```yaml
services:
  zapret-dns:
    build: .
    container_name: zapret-dns
    restart: unless-stopped
    network_mode: host
    cap_add:
      - NET_BIND_SERVICE
    volumes:
      - ./config.yaml:/etc/zapret-dns/config.yaml:ro
      - ./cache:/var/cache/zapret-dns
    environment:
      - CONFIG=/etc/zapret-dns/config.yaml
```

---

## Нефункциональные требования

- Язык: **Go 1.22+**
- Внешние зависимости: `miekg/dns` (DNS протокол), `v2fly/v2ray-core` (парсинг .dat), `yaml.v3` (конфиг), `prometheus/client_golang` (метрики)
- Покрытие тестами: unit-тесты для `fakeip`, `matcher`, `cache`
- Время ответа: p99 < 5ms для fakeip (без upstream запроса)
- Потребление памяти: < 128MB при 500k доменов в правилах

---

## Пример поведения

```
Запрос: A instagram.com
→ матчинг: instagram.com совпадает с тегом zapret → action: fakeip
→ hash("instagram.com") % size(198.18.0.0/15) → 198.18.42.17
→ ответ: A 198.18.42.17 TTL 1

Запрос: A google.com
→ матчинг: нет совпадений
→ upstream: параллельный запрос tls://1.1.1.1, tls://77.88.8.8
→ первый ответ: A 142.250.74.46 TTL 300
→ кэш: сохранить на 300 секунд
→ ответ: A 142.250.74.46 TTL 300

Запрос: A ads.example.com
→ матчинг: совпадает с тегом ads → action: block
→ ответ: NXDOMAIN
```