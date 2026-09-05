# Changelog

All notable changes to `anythingllm-mcp-gateway` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [0.2.0] - 2026-09-05

### Added
- Complete rewrite of the Data Plane Semantic Gateway from Python/FastMCP to Go 1.25 using `mark3labs/mcp-go`.
- MCP tools: `search_memory`, `store_memory`, `get_document`, `gateway_health`.
- Hybrid search fusion engine combining AnythingLLM vector search with local SQLite FTS5 BM25 lexical ranking.
- Reciprocal Rank Fusion (RRF) with temporal decay penalties for stale documentation.
- Adaptive token budgeting with boundary-aware sentence/paragraph trimming.
- Multi-stage Go `Dockerfile` and automated `Makefile`.

### Changed
- Shifted default AnythingLLM REST API target from `http://127.0.0.1:3001/api/v1` to `http://127.0.0.1:3002/api/v1`.
- Reduced memory footprint from ~85 MB (Python) to ~13 MB (Go).

### Removed
- Deprecated Python FastMCP runtime, virtual environments, and PyPI build scripts.
