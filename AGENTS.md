# AI Agents Instructions (AGENTS.md)

Welcome to the `anythingllm-mcp-gateway` project. This document provides important context and guidelines for autonomous agents operating within this repository.

## Repository Role

`anythingllm-mcp-gateway` is a Model Context Protocol (MCP) server for AnythingLLM semantic memory integration. It acts as a semantic memory layer in the Antigravity Agent Ecosystem (within the DoctorM & Ai / TheNovaNodes Ecosystem).

## Core Principles

1.  **Code Quality**: Write clean, maintainable, modular, and idiomatic Python 3.10+ code adhering to standard PEP 8 guidelines.
2.  **Language**: All documentation, docstrings, and comments **MUST** be written in high-quality, professional English. Do not write or leave comments in any other language.
3.  **Documentation Boundaries**: When writing documentation, rely only on existing, implemented code. Do not invent missing features; instead, mark them with a `### TODO` block if a feature is explicitly missing but mentioned as required context.
4.  **Resilience**: Ensure functions handle unexpected edge cases gracefully (e.g., empty files, missing environment variables).
5.  **Backward Compatibility**: Maintain full backward compatibility unless a breaking change is explicitly requested by the user.

## Testing Guidelines

*   Run the test suite using the command: `pytest tests/ -v` (or standard `python3 -m unittest discover -s tests`).
*   Zero test failures are allowed before committing code.
*   Add unit/regression tests for every new feature or bugfix.

## Commit Guidelines

*   Follow the Conventional Commits format: `feat(scope): ...`, `fix(scope): ...`, `docs(scope): ...`, etc.
*   Ensure PR descriptions are detailed and summarize the Problem Statement, Root Cause, Solution Details, and Verification Results.
