## list-updater

Кеширующий прокси для списков ("original" + "plain").

### Config

File: `config.yaml` (YAML).

Implementation note:

- Entry point: `infra-helper/cmd/listUpdater/cmd.go`
- Implementation: `infra-helper/cmd/listUpdater/internal/listupdater`

Example:

```yaml
dir: ./data
listen: :8080
refresh: 1h
lists:
  - name: rkn.dat
    url: https://github.com/kutovoys/ru_gov_zapret/releases/latest/download/zapret.dat
```

Behavior:

- Original is saved to `dir/original/<name>` and served at `/<name>`.
- Plain is saved to `dir/plain/<name>` and served at `/plain/<name>`.
- Category plain (only for .dat sources with categories) is served at `/plain/<name>/<category>`.
- Category list (only for .dat sources) is served at `/plain/<name>/`.
- If the original is a protobuf .dat (GeoSiteList), plain is generated as one domain per line.
- If the original is already plaintext, `/plain/...` serves the same content as-is.

---

## Opisanie (RU)

`list-updater` - это сервис, который:

- по конфигурации `config.yaml` периодически скачивает списки по URL
- кеширует оригинал в `dir/original/<name>`
- готовит "плоский" (plain) вариант в `dir/plain/<name>`
- отдаёт:
  - `GET /<name>` - оригинал
  - `GET /plain/<name>` - plain (по 1 домену/IP на строку)
  - `GET /plain/<name>/` - список доступных категорий (по одной на строку)
  - `GET /plain/<name>/<category>` - plain только для одной категории (если источник .dat содержит категории)

Пример (аналог `wget -O zapret.dat ...`, но с именем `rkn.dat` в сервисе):

```yaml
dir: ./data
listen: :8080
refresh: 1h
lists:
  - name: rkn.dat
    url: https://github.com/kutovoys/ru_gov_zapret/releases/latest/download/zapret.dat
```

Детали plain-конверсии:

- если оригинал - protobuf `.dat` (GeoSiteList), `/plain/...` строится из доменов (dedup + sort)
- если оригинал уже текстовый (1 значение на строку), `/plain/...` отдаёт его же

Категории (для `.dat`):

В zapret.dat категории используются так:

```
ext:zapret.dat:zapret
ext:zapret.dat:zapret-zapad
```

В `list-updater` категории можно скачать по:

- `GET /plain/<name>/zapret`
- `GET /plain/<name>/zapret-zapad`
