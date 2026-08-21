# Antigravity Agent Instructions

This document provides instructions for AI agents working within the Antigravity Agent Ecosystem on this repository (`anythingllm-mcp-gateway`).

## General Directives

- **Code Quality:** Ensure all code additions adhere to standard Python PEP 8 conventions.
- **Language:** All comments, docstrings, and documentation must be written in high-quality English.
- **Architecture Compliance:** Before modifying core logic in `memory_gateway/search.py`, review `ARCHITECTURE.md` to ensure your changes align with the hybrid search, scoring, and context assembly designs.

## Testing

When introducing new features or fixing bugs:
1.  Add corresponding unit tests in the `tests/` directory.
2.  Ensure tests can be run via standard `python3 -m unittest discover -s tests`.
3.  If modifying core mechanisms (like token management or API wrappers), consider mocking external dependencies.

## Documentation

- Ensure `README.md` is updated if there are changes to the deployment steps, environment variables, or tool parameters.
- Add descriptive docstrings to all newly introduced functions and classes.
