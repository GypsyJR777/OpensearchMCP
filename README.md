# OpensearchMCP

MCP-сервер для OpenSearch на Go: один статический бинарник в distroless-образе (~20 МБ), транспорты stdio и Streamable HTTP, режим только для чтения по умолчанию.

## Инструменты

| Инструмент | Что делает |
|---|---|
| `cluster_info` | Имя кластера, версия OpenSearch |
| `cluster_health` | Статус green/yellow/red, ноды, шарды; опционально по индексу и с `level` |
| `list_nodes` | Ноды: роли, cluster manager, heap/RAM/CPU/диск, версия |
| `list_indices` | Индексы: health, status, pri/rep, docs.count, store.size |
| `get_mapping` | Маппинг полей индекса |
| `get_index` | Алиасы, маппинги и настройки индекса |
| `list_aliases` | Алиасы и индексы, на которые они указывают |
| `list_shards` | Шарды: состояние, нода, причина unassigned |
| `search` | Поиск на Query DSL: query, aggs, sort, `_source` и т.д. |
| `count` | Количество документов по запросу |
| `get_document` | Документ по ID, с фильтрацией `_source` |

Только при `OPENSEARCH_READ_ONLY=false`:

| Инструмент | Что делает |
|---|---|
| `index_document` | Создать или заменить документ |
| `delete_document` | Удалить документ |
| `create_index` | Создать индекс с settings, mappings и aliases |

Удаления индексов нет намеренно.

## Запуск

### stdio (клиент запускает контейнер сам)

```json
{
  "mcpServers": {
    "opensearch": {
      "command": "docker",
      "args": ["run", "-i", "--rm",
               "-e", "OPENSEARCH_URL=https://opensearch.example.com:9200",
               "-e", "OPENSEARCH_USERNAME", "-e", "OPENSEARCH_PASSWORD",
               "opensearch-mcp:latest"],
      "env": { "OPENSEARCH_USERNAME": "reader", "OPENSEARCH_PASSWORD": "..." }
    }
  }
}
```

### HTTP (сервис в Docker или Kubernetes)

```sh
docker run -d -p 8080:8080 \
  -e MCP_TRANSPORT=http \
  -e MCP_HTTP_AUTH_TOKEN=change-me \
  -e OPENSEARCH_URL=https://opensearch:9200 \
  opensearch-mcp:latest
```

Эндпоинты:

- `/mcp`: MCP Streamable HTTP. Работает в stateless-режиме, поэтому можно держать несколько реплик за балансировщиком без sticky-сессий.
- `/healthz`: liveness, процесс жив.
- `/readyz`: readiness, OpenSearch доступен.

Если задан `MCP_HTTP_AUTH_TOKEN`, `/mcp` требует заголовок `Authorization: Bearer <token>`.

## Конфигурация

Все параметры задаются через переменные окружения. Для секретов поддерживается суффикс `_FILE`, например `OPENSEARCH_PASSWORD_FILE=/run/secrets/os_password`.

| Переменная | По умолчанию | Описание |
|---|---|---|
| `OPENSEARCH_URL` | `http://localhost:9200` | Адреса нод через запятую |
| `OPENSEARCH_USERNAME` / `OPENSEARCH_PASSWORD` | — | Basic auth |
| `OPENSEARCH_CA_CERT` | — | Путь к PEM с CA |
| `OPENSEARCH_CLIENT_CERT` / `OPENSEARCH_CLIENT_KEY` | — | mTLS, клиентский сертификат и ключ |
| `OPENSEARCH_INSECURE_SKIP_VERIFY` | `false` | Отключить проверку TLS (только для разработки) |
| `OPENSEARCH_TIMEOUT` | `30s` | Таймаут одной попытки запроса |
| `OPENSEARCH_MAX_RETRIES` | `3` | Ретраи на 502/503/504; `0` отключает |
| `OPENSEARCH_READ_ONLY` | `true` | Скрыть инструменты записи |
| `OPENSEARCH_ALLOWED_INDICES` | — (всё) | Allowlist паттернов через запятую, например `logs-*,metrics` |
| `OPENSEARCH_MAX_RESPONSE_BYTES` | `524288` | Ответ больше лимита возвращается ошибкой с подсказкой сузить запрос |
| `OPENSEARCH_MAX_SEARCH_SIZE` | `100` | Максимальный `size` в `search` |
| `MCP_TRANSPORT` | `stdio` | `stdio` или `http` |
| `MCP_HTTP_ADDR` | `:8080` | Адрес HTTP-сервера |
| `MCP_HTTP_AUTH_TOKEN` | — | Bearer-токен для `/mcp` |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. Логи в JSON в stderr |

### Allowlist индексов

При заданном `OPENSEARCH_ALLOWED_INDICES`:

- запросы к индексам вне списка отклоняются ещё до обращения к OpenSearch;
- wildcard-запрос разрешён, только если allowlist покрывает все индексы, в которые он может раскрыться: `logs-2024*` при `logs-*` разрешён, `*` нет;
- `list_indices`, `list_aliases` и `list_shards` фильтруют результат построчно;
- `_all`, исключения (`-index`), date math и cross-cluster (`remote:index`) запрещены всегда.

Allowlist проверяет имена в запросе. Алиас, имя которого попадает в allowlist, даёт доступ к индексам, на которые он указывает. Для строгой изоляции используйте также роли security-плагина OpenSearch.

## Разработка

```sh
make test          # unit-тесты
make up            # OpenSearch + MCP (http://localhost:8080/mcp) в docker compose
make integration   # все инструменты против реального OpenSearch
make image         # собрать образ
make down
```
