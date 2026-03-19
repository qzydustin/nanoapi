# nanoapi

Lightweight LLM gateway. Single config file, multi-provider routing, protocol translation, usage tracking.

## Why nanoapi

Use Claude Code, Cursor, or any OpenAI/Anthropic client with any backend — Anthropic, OpenAI, Open WebUI, LiteLLM, or any compatible endpoint. One config file, zero client-side changes.

## Features

**Protocol translation** — Clients speak OpenAI or Anthropic; upstream can be either. Requests and responses are translated through a protocol-neutral canonical layer.

**Web search across protocols** — Translates web search tools between Anthropic (`web_search_20250305`) and OpenAI (`web_search` + `search_context_size`). When the upstream is OpenAI-compatible, nanoapi synthesizes Anthropic-native `server_tool_use` and `web_search_tool_result` blocks so Claude Code displays search activity and results correctly. For Open WebUI backends, search results (URLs, titles) are extracted from the `sources` event and populated into the synthesized blocks.

**Raw block passthrough** — Unrecognised Anthropic content block types are preserved as raw JSON through the canonical layer and emitted verbatim. New server tools work without code changes.

**Reasoning / thinking control** — Cross-protocol reasoning translation: Anthropic thinking mode/budget maps to OpenAI reasoning effort and vice versa. Per-provider and per-model override of thinking mode, budget tokens, and effort level. Allowed effort whitelist prevents unsupported modes from reaching the upstream.

**Model aliasing and priority routing** — Map any client model name to any upstream model (e.g. `gpt-4o-mini` to `claude-haiku-4-5-20251001`). When multiple providers serve the same model, highest priority wins.

**Force-stream aggregation** — Stream from upstream even when the client requests non-streaming, then reassemble into a single response. Useful for backends that only support streaming.

**Provider quirks** — Per-provider flags for non-standard behavior, e.g. `openwebui_websearch` sends web search as a feature flag instead of a tool definition.

**Usage tracking and debug logging** — SQLite-backed per-request tracking: tokens, cache, reasoning, latency, status. Optional full request/response packet dump for debugging.

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
- **providers.quirks** — non-standard backend behavior (e.g. `openwebui_websearch: true`)
- **override.defaults / override.rules** — provider-level and per-model parameter overrides (max_tokens, temperature, reasoning, etc.)
- **logging** — `debug: true` writes full packets to `request_dir`

## Testing

```bash
go test ./...
```
