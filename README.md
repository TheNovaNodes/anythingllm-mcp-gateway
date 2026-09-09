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

High-performance Go-based Model Context Protocol (MCP) server for AnythingLLM semantic memory integration (`TheNovaNodes/anythingllm-mcp-gateway`). Provides AI agents with hybrid search (dense vector embeddings + lexical FTS5 BM25), reciprocal rank fusion (RRF), adaptive token budgeting, and multi-tenant organization isolation.

---

## 🛠️ Exposed MCP Tools

The gateway exposes 3 high-level semantic memory tools:

- **`search_memory`**  
  FTS5-First hybrid search querying AnythingLLM vector indices and local FTS5 lexical storage, fusing results with Reciprocal Rank Fusion (RRF), exact match boosting, and context assembly. Features candidate workspace pruning to prevent thundering herd timeouts across large multi-project clusters.  
  *Arguments:*  
  — `query` (string, required): The search text or question.  
  — `top_k` (int, optional): Maximum number of passages to return (default: 5, max: 25).  
  — `workspace` (string, optional): Target AnythingLLM workspace slug (defaults to automatic FTS5-First candidate routing).  
  — `expand_context` (bool, optional): Expand matching passages to full surrounding paragraphs.  
  — `max_token_budget` (int, optional): Token budget limit; results are trimmed on sentence boundaries.  
  — `tier` (string, optional): Memory tier filter (`episodic`, `semantic`, `procedural`).  
  — `vector_weight` (float, optional): Weight multiplier for vector retrieval layer in RRF (default: 1.0).  
  — `lexical_weight` (float, optional): Weight multiplier for lexical FTS5 layer in RRF (default: 1.0).  
  — `min_vector_similarity` (float, optional): Cosine similarity cutoff for pure-vector hits (default: 0.55).  
  — `max_context_chars` (int, optional): Maximum characters for paragraph context expansion (default: 4000).

- **`get_document`**  
  Retrieves full raw document text directly from the local lexical SQLite index by document ID (<1ms latency, zero HTTP overhead).  
  *Arguments:*  
  — `doc_id` (string, required): Unique document identifier or file path.  
  — `workspace` (string, optional): Target workspace slug filter.  
  — `max_chars` (int, optional): Truncation limit in characters (default: 20000).

- **`gateway_health`**  
  Diagnostics probe that verifies AnythingLLM REST API reachability, checks vector layer latency, tests lexical database integrity, and reports operational status.  
  *Arguments:* None.


---

## ⚡ Key Features

- **Multi-Tenant Organization Isolation:** Filters candidates by allowed organizational scope (`MG_ALLOWED_ORGS`) before RRF fusion, preventing data leakage across distinct projects.
- **FTS5 Morphological Stemming & Wildcards:** Intelligent query expansion generating exact tokens, prefix wildcards (`word*`), and morphological stems for Russian inflections and English plurals/tenses, dramatically boosting lexical recall.
- **Hybrid Synergy Multiplier:** Automatically amplifies the rank score (+25% bonus) of documents corroborated by both vector semantic and lexical FTS5 layers.
- **Smart Workspace Candidate Routing:** Pre-filters candidate workspaces dynamically using lexical matches and query-token slug heuristics to prevent thundering herd timeouts across dozens of workspaces.
- **BM25 Compound Tokenizer:** Advanced FTS5 query parser that splits `camelCase`, `PascalCase`, `kebab-case`, `snake_case`, and hyphenated terms (`ChaCha20Poly1305`, `agent-vault`) into exact sub-tokens.
- **SQLite FTS5 Column Weighting:** Applies custom BM25 column weights (`title=10.0`, `path=5.0`, `content=1.0`) to give document titles priority over long body text.
- **Vector Drift Sanitization:** Filters out orthogonal vector noise (distance $\ge 0.85$ or score 1.0 distance inversion anomalies) before rank fusion.
- **Exact RRF Boost:** Adds score-based rank bonuses (+0.02 to +0.03) for high-confidence BM25 hits and exact title/path substring matches.
- **Adaptive Token Budgeting:** Trims retrieved passages on natural sentence and paragraph boundaries when `max_token_budget` is set.
- **Context Assembly & 48-Token Snippets:** Returns rich 48-token context snippets with highlight markers and expands snippet hits to full surrounding paragraph context for coherent agent reasoning.
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
The compiled binaries will be placed in `./bin/`:
- `bin/anythingllm-gateway` — MCP stdio search server
- `bin/anythingllm-sync` — Autonomous background ETL sync daemon

### Install System-wide
```bash
sudo cp bin/anythingllm-gateway /usr/local/bin/
sudo cp bin/anythingllm-sync /usr/local/bin/
```

### Health Check (stdio smoke test)
```bash
anythingllm-gateway < /dev/null
anythingllm-sync -once
```

---

## ⚙️ Configuration & Environment Variables

- **`MG_ALM_BASE`** (or `ANYTHINGLLM_BASE_URL`)  
  AnythingLLM REST API base endpoint.  
  *Default:* `http://127.0.0.1:3002/api/v1`
- **`MG_API_KEY`** (or `ANYTHINGLLM_API_KEY`)  
  AnythingLLM Bearer API key.
- **`MG_ALLOWED_ORGS`**  
  Comma-separated list of organization slugs allowed for multi-tenant isolation (e.g. `thenovanodes`, `thedoctormes-hue`).
- **`MG_WORKSPACE`** (or `MG_DEFAULT_WORKSPACE`)  
  Default workspace slug for search and storage.  
  *Default:* `default`
- **`MG_LEXICAL_DB`**  
  Optional path to SQLite database for FTS5 lexical search.  
  *Default:* `/root/projects/TheNovaNodes/ops/shared/anythingllm-sync/lexical.db`
- **`MG_LEXICAL_MIN_SCORE`**  
  Minimum lexical score threshold (float, default: `0.0`).
- **`MG_MIN_VECTOR_SIMILARITY`**  
  Minimum cosine similarity threshold for pure-vector candidates without lexical corroboration (float, default: `0.55`). Discards out-of-domain noise.
- **`MG_VECTOR_SCORE_THRESHOLD`**  
  Minimum AnythingLLM vector search score threshold (float, default: `0.13`).
- **`MG_RRF_K`**  
  Reciprocal Rank Fusion smoothing constant (int, default: `60`).
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
