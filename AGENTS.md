# Guidelines for AI Agents

Welcome, AI Agents! This repository is an infrastructure component of the **Antigravity Agent Ecosystem** developed by TheNovaNodes.

When contributing or interacting with this codebase, observe the following directives:

---

## Part 1: NovaNodes Universal Collective Invariants

1. **Strict Git Flow (Правила Крови):** NEVER push directly to `main` or `master`. All changes must be made via a dedicated feature branch and proposed as a PR. No merge without ЗавЛаб approval.
2. **Zero Hardcoded Credentials:** Absolute security hygiene. All tokens and secrets must be dynamically loaded via environment variables (e.g., `MG_API_KEY`). No hardcoded secrets in tests or PRs.
3. **Network & Command Timeouts:** Strict anti-deadlock guardrails. Always use explicit context timeouts (`context.WithTimeout`) and handle network failures gracefully.
4. **Headless Linux Environment & Verification Without Absurdity:** Always verify your work correctly using stdio redirection (`< /dev/null`) when testing long-running or interactive commands. Ensure verification scripts do not stall.

---

## Part 2: Repository Specific Architecture & Profile

- **Technology Stack:** Go 1.25+, pure Go (zero CGO) with `modernc.org/sqlite`, and `mark3labs/mcp-go` for the stdio MCP server.
- **Canonical Exposed Tools:** The gateway exposes EXACTLY 3 MCP tools:
  - `search_memory`
  - `get_document`
  - `gateway_health`
- **CRITICAL NOTE:** The legacy `store_memory` tool has been permanently **amputated**. The gateway is a read-only query plane for AI agents; background ETL synchronization is handled by the dedicated daemon `cmd/anythingllm-sync`.
- **Hybrid Search Pipeline:** Employs FTS5-First candidate routing, combining dense vector embeddings with lexical SQLite FTS5 BM25. Uses Reciprocal Rank Fusion (RRF k=60), Hybrid Synergy Multiplier (+25%), exact match boosting (+0.02 to +0.03), pure-vector cosine similarity cutoff (default 0.55 via `MG_MIN_VECTOR_SIMILARITY`), and adaptive token budgeting on natural sentence boundaries.
- **Multi-Tenant Organization Isolation:** Candidate filtering is managed via `MG_ALLOWED_ORGS` prior to RRF fusion to prevent cross-tenant data leakage.
- **Binary Roles:**
  - `bin/anythingllm-gateway`: The high-performance MCP stdio server.
  - `bin/anythingllm-sync`: The autonomous background ETL synchronization daemon.

---

## Part 3: Golden Loop & Operational Standards

ALWAYS run The Golden Loop before declaring task completion:
1. **Pre-commit Checks:** Run unit and race tests:
   ```bash
   go vet ./...
   go test -v -race ./...
   ```
2. **Smoke Test:** Build and test the stdio server interactively without stalling:
   ```bash
   make build
   ./bin/anythingllm-gateway < /dev/null
   ```
