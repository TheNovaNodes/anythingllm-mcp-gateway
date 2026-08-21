# 📐 anythingllm-mcp-gateway Architecture & Design Specification

> **Author**: Trickster (`trickster@labdoctorm.ru`)
> **Organization**: TheNovaNodes
> **Version**: v0.2.0 (Product-Ready)
> **Stack**: Python 3.10+ / FastMCP / SQLite FTS5 / AnythingLLM REST API

---

## 🏛️ 1. Gateway Architecture Overview

**`anythingllm-mcp-gateway`** (`TheNovaNodes/anythingllm-mcp-gateway`) is an industrial-grade gateway using the **Model Context Protocol (MCP)**, providing high-performance access to semantic memory for AI agents.

```
                               ┌─────────────────────────┐
                               │  MCP Client (agy / LLM) │
                               └────────────┬────────────┘
                                            │ (FastMCP JSON-RPC)
                                            ▼
                               ┌─────────────────────────┐
                               │ anythingllm-mcp-gateway │
                               │        (v0.2.0)         │
                               └────────────┬────────────┘
                                            │
               ┌────────────────────────────┴────────────────────────────┐
               ▼                                                         ▼
┌──────────────────────────────┐                          ┌──────────────────────────────┐
│  Vector Layer (AnythingLLM)  │                          │    Lexical Layer (FTS5)      │
│  • REST API /vector-search   │                          │  • SQLite docs_fts (BM25)    │
│  • Fan-out Throttle (Sem)    │                          │  • Read-Only Access (mode=ro) │
│  • Memory Cache (TTL 120s)   │                          │  • Paragraph Context Assembly│
└──────────────┬───────────────┘                          └──────────────┬───────────────┘
               │                                                         │
               └────────────────────────────┬────────────────────────────┘
                                            ▼
                               ┌─────────────────────────┐
                               │   Hybrid Fusion Engine  │
                               │ • Score Calibration     │
                               │ • Temporal Decay Scale  │
                               │ • Token Budget Trimmer  │
                               │ • Dependency Graphing   │
                               └─────────────────────────┘
```

---

## 🔬 2. Mathematical and Algorithmic Models

### 2.1. Score-Calibrated Weighted Fusion
Instead of standard RRF, which normalizes positions to $1/(k+rank)$, the gateway applies a score normalization in the range of $[0, 1]$:

$$S_{fused} = \alpha \cdot \text{MinMax}(S_{vec}) + (1 - \alpha) \cdot \text{MinMax}(S_{lex})$$

Where $\alpha = 0.6$ by default, giving a slight priority to the semantic vector while maintaining the weight of exact lexical matches from FTS5.

### 2.2. Temporal Decay Scaling
To prevent the agent from using outdated code rules and decisions, an exponential decay factor is applied to the final score:

$$D_{temporal} = \max\left(0.6, \exp(-\lambda \cdot \Delta t_{days})\right)$$

where $\lambda = 0.005$. A document created today gets a value of $1.0$, while a six-month-old document gradually reduces its weight to a threshold of $0.6$.

### 2.3. Adaptive Token Budgeting
When calling `search_memory` with the `max_token_budget` parameter:
1. The results sequentially sum up the token volumes ($\approx \text{chars} / 4$).
2. Upon reaching the threshold value, the last fragment is neatly trimmed at sentence and word boundaries, adding a `trimmed_to_budget: true` marker.

---

## 🛠️ 3. MCP Tools Specification

### `search_memory`
- **Parameters**: `query`, `top_k`, `workspace`, `expand_context`, `max_token_budget`, `tier`.
- **Output**: `{query, count, results[], degraded, layers, total_estimated_tokens}`.

### `store_memory` (Active Ingestion)
- **Parameters**: `content`, `title`, `workspace`, `metadata`, `tier`.
- **Mechanics**: Direct upload via `/api/v1/document/raw-text` and synchronization of workspace embeddings via `/update-embeddings`.

### `get_document`
- **Parameters**: `doc_id`, `max_chars`.
- **Mechanics**: Extraction of full raw data of the document from `docs_fts`.

### `gateway_health`
- **Mechanics**: A real test probe of the hybrid search, reporting on *degraded mode* in case the vector layer goes down.

---

## 🧪 4. Testing

```bash
python3 -m unittest discover -s tests
```
All tests are executed in an isolated temporary directory with valid access token generation.
