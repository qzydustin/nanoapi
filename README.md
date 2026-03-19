# nanoapi

`nanoapi` is a lightweight LLM gateway that routes requests to different upstream providers from a single config file and records internal usage / logs.

Supported endpoints:

- OpenAI Chat Completions compatible endpoint: `/v1/chat/completions`
- Anthropic Messages compatible endpoint: `/v1/messages`
- Gateway API endpoints:
  - `/api/health`
  - `/api/usage`
  - `/api/logs`

## Features

- Provider selection by `model`, with `priority` used when multiple providers match
- Support for `openai_chat` and `anthropic_messages`
- Streaming, non-streaming, and `force_stream` aggregation
- OpenAI / Anthropic protocol translation
- Static token configuration from `config.yaml`
- SQLite persistence for usage / logs

## Configuration

Create `config.yaml` from the example:

```bash
cp config.example.yaml config.yaml
```

Fill in at least:

- `tokens[].key`
- `providers[].api_key`
- optionally `logging.debug` if you want full request/response packet logs

For Docker Compose, the default SQLite path is `/app/data/nanoapi.db`.
Provider overrides use `override.defaults` and optional ordered `override.rules`.

### Logging

```yaml
logging:
  debug: false
  request_dir: logs/requests
```

- `debug: false`
  - prints compact terminal logs such as `access | ...`, `reasoning | ...`, and `upstream_result | ...`
- `debug: true`
  - still keeps compact terminal logs
  - additionally writes full request / response packets to `logs/requests/<request_id>.log`
  - includes raw upstream SSE lines for streaming requests
- `request_dir`
  - controls where per-request debug files are written
  - use a persistent path such as `/app/data/request-logs` in Docker

## Run Locally

```bash
go run . ./config.yaml
```

Or:

```bash
go build -o nanoapi .
./nanoapi ./config.yaml
```

## Docker Compose

```bash
docker compose up --build -d
```

View logs:

```bash
docker compose logs -f nanoapi
```

Stop:

```bash
docker compose down
```

## API Examples

```bash
curl http://127.0.0.1:8080/api/health
```

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer nk_replace_me" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

```bash
curl http://127.0.0.1:8080/v1/messages \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer nk_replace_me" \
  -d '{
    "model": "claude-haiku-4-5-20251001",
    "max_tokens": 256,
    "messages": [
      {"role": "user", "content": "hello"}
    ]
  }'
```

```bash
curl http://127.0.0.1:8080/api/usage \
  -H "Authorization: Bearer nk_replace_me"
```

```bash
curl http://127.0.0.1:8080/api/logs \
  -H "Authorization: Bearer nk_replace_me"
```

## Claude Code Request Shape

When Claude Code sends requests to `nanoapi` through the Anthropic Messages endpoint, the request usually contains these signals:

- `/effort`
  - Claude Code exposes four effort levels: `low`, `medium`, `high`, `max`
  - This appears in the request body as `output_config.effort`

- Thinking switch
  - When thinking is enabled, Claude Code sends `thinking.type = "adaptive"`
  - When thinking is disabled, Claude Code does not send `thinking.type`

- `1M context`
  - This appears in the request headers as `Anthropic-Beta: ... context-1m-2025-08-07 ...`

## Database

The database is used only for usage / log persistence: token ID, timestamp, client / upstream protocol, client / upstream model, token usage, cache usage, reasoning tokens, success / error code, and latency.

Tokens, providers, and API keys are loaded from `config.yaml`.

## Testing

```bash
go test ./...
```

## Current Scope

This project currently follows a config-driven gateway model: no admin web UI, no dynamic token CRUD API, and configuration changes are applied by editing `config.yaml` and restarting the service.
