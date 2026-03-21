# nanoapi

Lightweight gateway that proxies Anthropic Messages API and OpenAI Chat Completions API to OpenWebUI (OpenAI-compatible upstream).

## Features

- **Protocol translation** — Accepts both Anthropic Messages and OpenAI Chat Completions. Translates to OpenAI format for upstream, translates responses back to the client's protocol.
- **Dual entry points** — `/v1/messages` for Anthropic clients (Claude Code), `/v1/chat/completions` for OpenAI-compatible clients.
- **Web search** — Translates Anthropic `web_search` tool to OpenWebUI `features.web_search` flag. Synthesizes `server_tool_use` + `web_search_tool_result` blocks from OpenWebUI `sources` events so Claude Code displays results correctly.
- **Reasoning / thinking** — Maps Anthropic thinking mode/effort to OpenAI `reasoning_effort`. Effort mapping and allowed values are config-driven. Disabled thinking omits the field so upstream skips thinking entirely.
- **Model aliasing and fallback** — Map client model names to upstream models. Multiple providers can serve the same model with priority-based fallback on 5xx errors.
- **Force-stream aggregation** — Stream from upstream even when the client requests non-streaming, then reassemble into a single response.
- **Usage tracking** — JSONL-backed per-request tracking: tokens, cache, reasoning, latency, status.
- **Debug logging** — Optional full request/response packet dump to disk.

## Endpoints

| Path | Description |
|---|---|
| `/v1/messages` | Anthropic Messages (Claude Code) |
| `/v1/chat/completions` | OpenAI Chat Completions |
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

## Scripts

### Usage Report

Query per-token usage across all configured tokens:

```bash
./scripts/usage.sh <base_url> <config.yaml> [5h|24h|7d|30d]
```

Examples:

```bash
./scripts/usage.sh http://localhost:8080 config.yaml        # all time
./scripts/usage.sh http://localhost:8080 config.yaml 24h    # last 24 hours
./scripts/usage.sh http://localhost:8080 config.yaml 7d     # last 7 days
```

Output:

```
=== LAST 24 HOURS ===

TOKEN              REQS    INPUT   OUTPUT  CACHE_R  CACHE_W   REASON  LAST_USED
-----              ----    -----   ------  -------  -------   ------  ---------
claw                 42   1.2M     85.3K   956.1K    12.4K    5.6K   2026-03-21 08:30
coding               17   430.5K   32.1K   380.2K     8.1K    2.1K   2026-03-21 07:15
-----              ----    -----   ------  -------  -------   ------
TOTAL                59   1.6M    117.4K     1.3M    20.5K    7.7K
```

Requires `curl`, `jq`, and `awk`.
