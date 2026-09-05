# Contributing to anythingllm-mcp-gateway

Thank you for your interest in contributing to the **AnythingLLM Semantic Memory Gateway MCP Server**, part of the Antigravity Agent Ecosystem by TheNovaNodes!

---

## Getting Started

1. Fork the repository on GitHub.
2. Clone your fork locally.
3. Ensure you have **Go 1.25+** installed.
4. Verify tests pass: `make test`.

---

## Development Guidelines

- **Code Style:** Follow official Go formatting standards (`go fmt ./...`). Run `make lint` to verify with `go vet`.
- **Testing:** All new features or modifications must include comprehensive unit tests. Run `make test` (`go test -v -race ./...`) to ensure 100% pass rate and zero race conditions.
- **Documentation:** When modifying MCP tool schemas or algorithms, update `README.md` and `ARCHITECTURE.md`.
- **Strict Git Flow (Правила Крови):** NEVER push directly to `main` or `master`. Always create a dedicated branch and submit a PR.
