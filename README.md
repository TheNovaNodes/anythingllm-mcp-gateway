---
module_type: gateway
status: active
protocol: mcp
primary_capability: semantic_memory
requires: anythingllm
works_with: ai_agents, antigravity, mcp_clients
last_verified: 2026-09-05
---

# AnythingLLM Semantic Memory Gateway MCP Server 🧠

[![Go Version](https://img.shields.io/badge/go-1.25+-00ADD8.svg)](https://golang.org)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![MCP Server](https://img.shields.io/badge/MCP--Server-available-green)](https://modelcontextprotocol.io/)
[![Status: Active](https://img.shields.io/badge/Status-Active-brightgreen.svg)]()

High-performance Go-based Model Context Protocol (MCP) server for AnythingLLM semantic memory integration (`TheNovaNodes/anythingllm-mcp-gateway`). Provides AI agents with hybrid search (dense vector embeddings + lexical FTS5 BM25), reciprocal rank fusion (RRF), adaptive token budgeting, and real-time memory persistence.

---

## 🛠️ Exposed MCP Tools

The gateway exposes 4 high-level semantic memory tools:

- **`search_memory`**  
  Hybrid search querying AnythingLLM vector indices and local FTS5 lexical storage, fusing results with Reciprocal Rank Fusion (RRF), temporal decay scoring, and context assembly.  
  *Arguments:*  
  — `query` (string, required): The search text or question.  
  — `top_k` (int, optional): Maximum number of passages to return (default: 5, max: 25).  
  — `workspace` (string, optional): Target AnythingLLM workspace slug (defaults to configured workspace).  
  — `expand_context` (bool, optional): Expand matching passages to full surrounding paragraphs.  
  — `max_token_budget` (int, optional): Token budget limit; results are trimmed on sentence boundaries.  
  — `tier` (string, optional): Memory tier filter (`hot`, `warm`, `cold`).

- **`store_memory`**  
  Active write tool for uploading raw text, decision logs, and documentation into AnythingLLM vector memory. Automatically updates workspace embeddings.  
  *Arguments:*  
  — `content` (string, required): Raw text or markdown content to store.  
  — `title` (string, required): Human-readable title or identifier for the document.  
  — `workspace` (string, optional): Target workspace slug.  
  — `metadata` (object, optional): Key-value metadata attached to the record.  
  — `tier` (string, optional): Memory tier label.

- **`get_document`**  
  Retrieves full raw document text from the local lexical index by document ID.  
  *Arguments:*  
  — `doc_id` (string, required): Unique document identifier.  
  — `max_chars` (int, optional): Truncation limit in characters (default: 10000).

- **`gateway_health`**  
  Diagnostics probe that verifies AnythingLLM REST API reachability, checks vector layer latency, tests lexical database integrity, and reports operational status.  
  *Arguments:* None.

---

## ⚡ Key Features

- **Hybrid Fusion Engine:** Combines semantic vector similarity with SQLite FTS5 BM25 lexical ranking via score-calibrated Reciprocal Rank Fusion (RRF).
- **Adaptive Token Budgeting:** Prevents context window overflows by trimming retrieved passages on natural sentence and paragraph boundaries when `max_token_budget` is set.
- **Context Assembly:** Expands snippet hits to full surrounding paragraph context for coherent agent reasoning.
- **Temporal Decay:** Applies exponential decay penalties ($\lambda = 0.005$) to obsolete documentation so recent decisions rank higher.
- **Ultra-Low Overhead:** Written in pure Go (Go 1.25) with zero CGO dependencies (`modernc.org/sqlite`). Consumes ~13 MB RAM in production.

---

## 🚀 Quick Start & Building

### Prerequisites
- Go 1.25 or higher

### Build Binary
```bash
git clone https://github.com/TheNovaNodes/anythingllm-mcp-gateway.git
cd anythingllm-mcp-gateway
make build
```
The compiled binary will be placed at `./bin/anythingllm-gateway`.

### Install System-wide
```bash
sudo cp bin/anythingllm-gateway /usr/local/bin/
```

### Health Check (stdio smoke test)
```bash
anythingllm-gateway < /dev/null
```

---

## ⚙️ Configuration & Environment Variables

- **`MG_ALM_BASE`** (or `ANYTHINGLLM_BASE_URL`)  
  AnythingLLM REST API base endpoint.  
  *Default:* `http://127.0.0.1:3002/api/v1`
- **`MG_API_KEY`** (or `ANYTHINGLLM_API_KEY`)  
  AnythingLLM Bearer API key.
- **`MG_WORKSPACE`** (or `MG_DEFAULT_WORKSPACE`)  
  Default workspace slug for search and storage.  
  *Default:* `default`
- **`MG_LEXICAL_DB`**  
  Optional path to SQLite database for FTS5 lexical search.
- **`MG_LEXICAL_MIN_SCORE`**  
  Minimum lexical score threshold (float, default: `0.0`).
- **`MG_VECTOR_MAX_INFLIGHT`**  
  Maximum concurrent vector API calls to protect the AnythingLLM instance (default: `4`).
- **`MG_SEARCH_TIMEOUT`**  
  Search request timeout in seconds (default: `10`).

---

## 🔌 MCP Client Configuration

Add to your MCP client configuration (e.g., Claude Desktop, Antigravity, or `mcp-router`):

```json
{
  "mcpServers": {
    "anythingllm-gateway": {
      "command": "/usr/local/bin/anythingllm-gateway",
      "args": [],
      "env": {
        "MG_ALM_BASE": "http://127.0.0.1:3002/api/v1",
        "MG_API_KEY": "YOUR_API_KEY_HERE",
        "MG_WORKSPACE": "default"
      }
    }
  }
}
```

---

## 🧪 Testing

Run the full Go test suite with data race detection:
```bash
make test
```

Generate a code coverage report:
```bash
make coverage
```

---

## 📄 License

MIT License — see [LICENSE](LICENSE) for details.
