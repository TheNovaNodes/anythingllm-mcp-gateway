# anythingllm-mcp-gateway 🧠

[![Python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue)](https://www.python.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT/)
[![MCP Server](https://img.shields.io/badge/MCP--Server-available-green)](https://modelcontextprotocol.io/)
[![Status: Active](https://img.shields.io/badge/Status-Active-brightgreen)]

## About

High-performance **Model Context Protocol (MCP)** server for AnythingLLM semantic memory integration (`TheNovaNodes/anythingllm-mcp-gateway`). Provides AI agents with tools to search, retrieve, and actively store memory facts and documents into AnythingLLM workspaces through a clean, typed MCP API.

## 📚 Documentation

Detailed documentation for developers and operators is located in the `/docs` directory:

- [**Architecture & Design**](docs/architecture.md): Visualizes the data flows, algorithms (Score-Calibrated Weighted Fusion, Token Budgeting) and component interactions.
- [**API Reference**](docs/api-reference.md): Detailed documentation on all MCP exposed tools (`search_memory`, `store_memory`, `get_document`, `gateway_health`), request parameters, and response schemas.
- [**Deployment Guide**](docs/deployment.md): Step-by-step instructions for infrastructure setup, configuration, and environment variables.

---

## Exposed MCP Tools (Summary)

| Tool | Description |
|------|-------------|
| `search_memory` | Hybrid semantic search (vector + lexical FTS5, RRF fusion, Context Assembly, Adaptive Token Budgeting) |
| `store_memory` | **Active Write Tool**: Upload raw facts, session summaries, and text directly into AnythingLLM vector memory |
| `get_document` | Retrieve full raw text of a document by `doc_id` |
| `gateway_health` | Honest diagnostics: token presence, lexical DB, vector layer reachability, real hybrid search probe |

## Quick Start

### Installation

```bash
git clone https://github.com/TheNovaNodes/anythingllm-mcp-gateway.git
cd anythingllm-mcp-gateway
python3 -m venv venv
source venv/bin/activate
pip install -e .
```

### Configuration

Copy `.env.example` to `.env` and configure `MG_ALM_BASE` and `MG_TOKEN_FILE`. See the [**Deployment Guide**](docs/deployment.md) for detailed variable explanations.

### Testing

```bash
python3 -m unittest discover -s tests
```

## License

MIT — See LICENSE file.
