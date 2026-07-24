# nova-anythingllm-mcp

[![Python 3.10+](https://img.shields.io/badge/python-3.10%2B-blue)](https://www.python.org/)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![MCP Server](https://img.shields.io/badge/MCP--Server-available-green)](https://modelcontextprotocol.io/)
[![Status: Active](https://img.shields.io/badge/Status-Active-brightgreen)]

## About

MCP (Model Context Protocol) server for AnythingLLM semantic memory integration. Provides seamless access to lab-scale vector storage through 13 workspace-isolated endpoints, enabling AI agents to search, retrieve and inject documents into AnythingLLM workspaces.

## Features

- **Vector Search**: Full-text semantic search across 13 configurable workspaces
- **Workspace Isolation**: Each agent has dedicated workspace with private knowledge scope
- **Document Upload**: Inject documents into AnythingLLM with automatic chunking
- **Hybrid Search**: Vector similarity + lexical FTS5 ranking (RRF fusion)
- **Context Assembly**: Expands search results to full paragraph context
- **9 Tools Exposed**:
  - `search_memory` — Hybrid semantic search
  - `get_document` — Retrieve raw document content
  - `list_workspaces` — Available workspace enumeration
  - `gateway_health` — Diagnostics and connectivity check

## Installation

```bash
# Clone repository
git clone https://github.com/TheNovaNodes/nova-anythingllm-mcp.git
cd nova-anythingllm-mcp

# Create virtual environment
python3 -m venv venv
source venv/bin/activate

# Install dependencies
pip install -r requirements.txt
```

## Configuration

Creates `.env` from `.env.example`:

| Variable | Description | Default |
|----------|-------------|---------|
| `MG_AUTH_TOKEN` | Bearer token from AnythingLLM | Required |
| `MG_WORKSPACE_ID` | Workspace numeric ID (1) | 1 |
| `MG_EMBEDDING_MODEL` | Embedding model name | `multilingual-e5-small` |
| `MG_VECTOR_THRESHOLD` | Min vector score (0.0-1.0) | 0.3 |

## MCP Client Integration

Add to `claude_desktop_config.json`:

```json
{
  "mcpServers": {
    "nova-anythingllm": {
      "command": "python",
      "args": ["server.py"],
      "cwd": "/path/to/nova-anythingllm-mcp",
      "env": {
        "MG_AUTH_TOKEN": "your-bearer-token",
        "PYTHONPATH": "/path/to/nova-anythingllm-mcp"
      }
    }
  }
}
```

#,# 💖 Support TheNovaNodes,

If our MCP gateways save you time and expand your AI agents' capabilities, consider supporting our infrastructure and the development of new open-source integrations.
**USDT (TRC20): TQvw8MJMdSBFXu5G74JsZm1gzg7cuXBZ2o**

## License

MIT — See LICENSE file.
