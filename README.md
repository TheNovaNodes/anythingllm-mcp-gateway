# nova-anythingllm-mcp

[![Python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue)](https://www.python.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT/)
[![MCP Server](https://img.shields.io/badge/MCP--Server-available-green)](https://modelcontextprotocol.io/)
[![Status: Active](https://img.shields.io/badge/Status-Active-brightgreen)]

## About

MCP (Model Context Protocol) server for AnythingLLM semantic memory integration. Provides AI agents with tools to search, retrieve, and inject documents into AnythingLLM workspaces through a clean, typed API.

## Tools

| Tool | Description |
|------|-------------|
| `search_memory` | Hybrid semantic search (vector + lexical FTS5, RRF fusion with context assembly) |
| `get_document` | Retrieve full raw text of a document by `doc_id` |
| `gateway_health` | Diagnostics: token presence, lexical DB, vector layer reachability, search functionality |

## Features

- **Hybrid Search** — vector similarity + lexical BM25 ranking, fused via score-calibrated weighted merge (NDCG-validated)
- **Context Assembly** — expands search hits to full paragraph context for coherent agent input
- **Workspace Isolation** — optional workspace scoping for multi-agent setups
- **Gatekeeping** — real health probe that doesn't trust itself; reports degraded mode honestly
- **Fan-out Throttle** — configurable concurrency limits on vector calls to protect ALM

## Installation

```bash
git clone https://github.com/TheNovaNodes/nova-anythingllm-mcp.git
cd nova-anythingllm-mcp
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
```

## Configuration

Copy `.env.example` to `.env` and fill in your values:

```bash
cp .env.example .env
```

| Variable | Description | Default |
|----------|-------------|---------|
| `MG_ALM_BASE` | AnythingLLM REST API endpoint | `http://127.0.0.1:3002/api/v1` |
| `MG_TOKEN_FILE` | Path to AnythingLLM Bearer token file | **Required** (no default) |
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
    "nova-anythingllm-mcp": {
      "command": "python",
      "args": ["-m", "memory_gateway.server"],
      "cwd": "/path/to/nova-anythingllm-mcp",
      "env": {
        "MG_TOKEN_FILE": "/path/to/your/anythingllm_token.txt",
        "MG_ALM_BASE": "http://127.0.0.1:3002/api/v1",
        "PYTHONPATH": "/path/to/nova-anythingllm-mcp"
      }
    }
  }
}
```

## Transport

| Transport | Use case |
|-----------|----------|
| `stdio` | Default — run as subprocess from MCP client config |
| `streamable-http` | Network deployment — runs on `MG_HOST:MG_PORT` |

## License

## License

MIT — See LICENSE file.
