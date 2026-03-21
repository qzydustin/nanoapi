# nanoapi

Lightweight gateway that proxies Claude Code (Anthropic Messages API) to OpenWebUI (OpenAI-compatible upstream).

## Features

- **Protocol translation** — Anthropic Messages in, OpenAI Chat Completions out. Requests and responses translated bidirectionally.
- **Web search** — Translates Anthropic `web_search` tool to OpenWebUI `features.web_search` flag. Synthesizes `server_tool_use` + `web_search_tool_result` blocks from OpenWebUI `sources` events so Claude Code displays results correctly.
- **Reasoning / thinking** — Maps Anthropic thinking mode/effort to OpenAI `reasoning_effort`. Effort mapping and allowed values are config-driven. Disabled thinking omits the field so upstream skips thinking entirely.
- **Model aliasing and fallback** — Map client model names to upstream models. Multiple providers can serve the same model with priority-based fallback on 5xx errors.
- **Force-stream aggregation** — Stream from upstream even when the client requests non-streaming, then reassemble into a single response.
- **Usage tracking** — SQLite-backed per-request tracking: tokens, cache, reasoning, latency, status.
- **Debug logging** — Optional full request/response packet dump to disk.

## Endpoints

| Path | Description |
|---|---|
| `/v1/messages` | Anthropic Messages (Claude Code) |
| `/api/health` | Health check |
| `/api/usage` | Per-token usage summary |
| `/api/logs` | Per-token request logs |

## Quick Start

```bash
cp config.example.yaml config.yaml
# fill in tokens[].key and providers[].api_key
go run . ./config.yaml
```

Docker Compose:

```bash
docker compose up --build -d
```

## Configuration

See [config.example.yaml](config.example.yaml) for a minimal setup and [config.full.example.yaml](config.full.example.yaml) for all fields.

Key sections:

- **tokens** — static API keys for client authentication
- **providers** — upstream backends with base_url, api_key, priority, model map
- **providers.models.reasoning** — `allowed_efforts` whitelist and `effort_map` for reasoning translation
- **override** — provider-level and per-model parameter overrides (`max_tokens`, `temperature`, `reasoning_effort`, etc.)
- **logging** — `debug: true` writes full packets to `request_dir`

## Testing

```bash
go test ./...
```
