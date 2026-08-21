---
module_type: gateway
status: active
protocol: mcp
primary_capability: semantic_memory
requires: anythingllm
works_with: ai_agents
last_verified: 2026-08-21
---
# anythingllm-mcp-gateway 🧠

[![Python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue)](https://www.python.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT/)
[![MCP Server](https://img.shields.io/badge/MCP--Server-available-green)](https://modelcontextprotocol.io/)
[![Status: Active](https://img.shields.io/badge/Status-Active-brightgreen)]

## About

High-performance **Model Context Protocol (MCP)** server for AnythingLLM semantic memory integration (`TheNovaNodes/anythingllm-mcp-gateway`). Provides AI agents with tools to search, retrieve, and actively store memory facts and documents into AnythingLLM workspaces through a clean, typed MCP API.

## Exposed MCP Tools

| Tool | Description |
|------|-------------|
| `search_memory` | Hybrid semantic search (vector + lexical FTS5, RRF fusion, Context Assembly, Adaptive Token Budgeting) |
| `store_memory` | **Active Write Tool**: Upload raw facts, session summaries, and text directly into AnythingLLM vector memory |
| `get_document` | Retrieve full raw text of a document by `doc_id` |
| `gateway_health` | Honest diagnostics: token presence, lexical DB, vector layer reachability, real hybrid search probe |

## Key Features

- **Active Memory Ingestion (`store_memory`)** — AI agents can write new facts, preferences, and documentation into AnythingLLM workspaces in real time.
- **Adaptive Token Budgeting** — Respects agent context budgets (`max_token_budget`), trimming passages cleanly on paragraph/sentence boundaries.
- **Hybrid Search** — Vector similarity + lexical BM25 ranking, fused via score-calibrated weighted merge (NDCG-validated).
- **Context Assembly** — Expands search hits to full paragraph context for coherent agent reasoning.
- **Workspace Isolation** — Optional workspace scoping for multi-agent setups.
- **Gatekeeping & Diagnostics** — Real health probe that tests hybrid search execution and reports degraded mode honestly.
- **Fan-out Throttle** — Configurable concurrency limits on vector calls to protect AnythingLLM instance.

## Installation

### From PyPI

```bash
pip install anythingllm-mcp-gateway
```

### Development

```bash
git clone https://github.com/TheNovaNodes/anythingllm-mcp-gateway.git
cd anythingllm-mcp-gateway
python3 -m venv venv
source venv/bin/activate
pip install -e .
```

## Configuration

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

| Variable | Description | Default / Example |
|----------|-------------|-------------------|
| `MG_ALM_BASE` | AnythingLLM REST API endpoint (Local: `http://127.0.0.1:3001/api/v1`, Prod: `https://alm.shtab-ai.ru/api/v1`) | `http://127.0.0.1:3001/api/v1` |
| `MG_TOKEN_FILE` | Path to AnythingLLM Bearer token file | **Required** (or `ANYTHINGLLM_API_KEY`) |
| `MG_OPS_DIR` | Operational directory for lexical index | `./ops` |
| `MG_LEXICAL_DB` | FTS5/BM25 lexical database path | `{MG_OPS_DIR}/lexical.db` |
| `MG_MAP_FILE` | Workspace slug mappings file | `{MG_OPS_DIR}/workspace_map.json` |
| `MG_VECTOR_SCORE_THRESHOLD` | Min vector score to accept (0.0–1.0) | `0.13` |
| `MG_DEFAULT_TOP_K` | Default number of search results | `5` |
| `MG_MAX_TOP_K` | Maximum search results per query | `25` |
| `MG_FUSION_MODE` | `weighted` or `rrf` | `weighted` |
| `MG_HOST` | MCP server host (streamable-http transport) | `127.0.0.1` |
| `MG_PORT` | MCP server port | `8091` |
| `MG_TRANSPORT` | `stdio` or `streamable-http` | `stdio` |
| `MG_LOG_LEVEL` | Logging level | `INFO` |

## MCP Client Integration

Add to your MCP client config (e.g. `claude_desktop_config.json`):

```json
{
  "mcpServers": {
    "anythingllm-mcp-gateway": {
      "command": "python",
      "args": ["-m", "memory_gateway.server"],
      "cwd": "/path/to/anythingllm-mcp-gateway",
      "env": {
        "ANYTHINGLLM_API_KEY": "YOUR_ANYTHINGLLM_API_KEY",
        "MG_ALM_BASE": "http://127.0.0.1:3001/api/v1"
      }
    }
  }
}
```

> **Note**: For production environments, point `MG_ALM_BASE` to `https://alm.shtab-ai.ru/api/v1`.

## Running Tests

```bash
python3 -m unittest discover -s tests
```

## License

MIT — See LICENSE file.
