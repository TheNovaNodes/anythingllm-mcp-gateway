# Changelog

All notable changes to `anythingllm-mcp-gateway` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.3.0] - 2026-09-10

### Added
- Semantic document chunking in background ETL daemon (`internal/etl/chunk.go`) with sliding window (512 tokens, 64-token overlap), sentence-boundary preservation, and granular metadata tracking (`#39`, `#58`).
- `group_by` retrieval parameter (`'chunk'` or `'document'`) for `search_memory` tool to consolidate passage scores per parent document (`#58`).
- Monolithic document reassembly on `get_document` from chunked SQLite FTS5 index (`#58`).
- Honest live functional probe in `gateway_health` verifying active vector search operational status.
- Comprehensive error tracking across candidate workspaces ensuring proper `degraded` flag propagation on vector search failures.

### Changed
- FinOps CI/CD optimization: added concurrency cancellation, 10-minute job timeout, shallow git fetch, path filtering, and Go dependency caching (`#56`, `#57`).
- Connection pooling enhancement: explicit response body draining on HTTP client to enable robust TCP socket recycling (`#55`).
- Updated `README.md` and `ARCHITECTURE.md` to document semantic chunking and `group_by` retrieval options.
- Updated `.env.example` with port 3002 default and removed deprecated Python orchestrator variables.

## [0.2.0] - 2026-09-05

### Added
- Complete rewrite of the Data Plane Semantic Gateway from Python/FastMCP to Go 1.25 using `mark3labs/mcp-go`.
- MCP tools: `search_memory`, `get_document`, `gateway_health`.
- Hybrid search fusion engine combining AnythingLLM vector search with local SQLite FTS5 BM25 lexical ranking.
- Reciprocal Rank Fusion (RRF) with temporal decay penalties for stale documentation.
- Adaptive token budgeting with boundary-aware sentence/paragraph trimming.
- Multi-stage Go `Dockerfile` and automated `Makefile`.

### Changed
- Shifted default AnythingLLM REST API target from `http://127.0.0.1:3001/api/v1` to `http://127.0.0.1:3002/api/v1`.
- Reduced memory footprint from ~85 MB (Python) to ~13 MB (Go).

### Removed
- Deprecated Python FastMCP runtime, virtual environments, and PyPI build scripts.
