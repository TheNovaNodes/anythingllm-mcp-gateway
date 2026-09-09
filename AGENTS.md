# Guidelines for AI Agents

Welcome, AI Agents! This repository is an infrastructure component of the **Antigravity Agent Ecosystem** developed by TheNovaNodes.

When contributing or interacting with this codebase, observe the following directives:

---

## Core Principles

1. **High-Quality English:** All code comments, documentation, and commit messages must be in high-quality English.
2. **Robustness & Timeouts:** Always use context timeouts (`context.WithTimeout`) and handle network failures gracefully when communicating with AnythingLLM or querying lexical databases.
3. **Go Standards:** Write idiomatic Go (Go 1.25+), check errors explicitly, avoid package-level global state, and enforce pure-vector cutoff thresholds (default: 0.55).
4. **Strict Git Flow (Правила Крови):** NEVER push directly to `main` or `master`. Always create a dedicated branch and open a PR.

---

## Development Workflow & Code Locations

- **MCP Tools:** Defined in `internal/server/server.go`. Use typed `mark3labs/mcp-go` tool definitions (`search_memory`, `get_document`, `gateway_health`).
- **REST Client Methods:** Placed in `internal/alm/client.go` with models in `internal/alm/types.go`.
- **Hybrid Retrieval & RRF Fusion:** Located under `internal/fusion/` (`rrf.go`, `budget.go`) and `internal/lexical/` (`db.go`, `tokenizer.go`).
- **Tests:** Placed alongside code (`*_test.go`) in their respective packages. Always run `go test -v -race ./...` before committing.
