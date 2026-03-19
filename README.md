# nanoapi

Lightweight LLM gateway — single config file, multi-provider routing, built-in usage tracking.

## Features

- **Protocol translation** — clients speak OpenAI or Anthropic; upstream can be either
- **Model aliasing** — map any client model name to any upstream model (e.g. `gpt-4o-mini` → `claude-haiku-4-5-20251001`)
- **Priority routing** — when multiple providers serve the same model, highest priority wins
- **Reasoning control** — per-provider / per-model override of thinking mode, budget tokens, effort level; per-model allowed effort whitelist
- **Force-stream aggregation** — optionally stream upstream even for non-stream client requests, then reassemble the response
- **Web search passthrough** — translate built-in web search tools across protocols (`openai` native or `openwebui` feature flag)
- **Usage & logs** — SQLite-backed per-request tracking: tokens, cache, reasoning, latency, status
- **Debug logging** — optional full request/response packet dump to per-request log files

## Endpoints

| Path | Description |
|---|---|
| `/v1/chat/completions` | OpenAI Chat Completions compatible |
| `/v1/messages` | Anthropic Messages compatible |
| `/api/health` | Health check |
| `/api/usage` | Per-token usage stats |
| `/api/logs` | Request logs |

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

See [config.example.yaml](config.example.yaml) for a minimal setup and [config.full.example.yaml](config.full.example.yaml) for all supported fields.

Key sections:

- **tokens** — static API keys for client authentication
- **providers** — upstream backends with protocol, base_url, api_key, priority, model map, and parameter overrides
- **override.defaults / override.rules** — provider-level and per-model parameter overrides (max_tokens, temperature, reasoning, etc.)
- **logging** — `debug: true` writes full packets to `request_dir`

## Testing

```bash
go test ./...
```
