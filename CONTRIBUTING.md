# Contributing to anythingllm-mcp-gateway

First off, thank you for considering contributing to `anythingllm-mcp-gateway`!

## Role of the Repository

`anythingllm-mcp-gateway` is a Model Context Protocol (MCP) server for AnythingLLM semantic memory integration and acts as a semantic memory layer in the Antigravity Agent Ecosystem (DoctorM & Ai / TheNovaNodes Ecosystem).

## How to Contribute

1.  **Fork and Clone**: Fork the repository and clone it to your local machine.
2.  **Environment Setup**: We recommend using a virtual environment.
    ```bash
    python3 -m venv venv
    source venv/bin/activate
    pip install -e .[dev]
    ```
3.  **Create a Branch**: Create a new branch for your feature or bug fix.
4.  **Make Changes**: Write your code. Ensure that all code, docstrings, and comments are written in high-quality, professional English. Follow PEP 8 guidelines.
5.  **Run Tests**: Verify your changes do not break existing functionality. We mandate zero test failures.
    ```bash
    pytest tests/ -v
    ```
6.  **Commit Guidelines**: Use Conventional Commits for your commit messages (e.g., `feat(search): add token limit`, `fix(config): resolve empty token issue`).
7.  **Submit a Pull Request**: Submit a PR to the main repository. Include a detailed description summarizing the problem, root cause, solution, and test results.

## Code Quality Standards

*   Ensure clean, modular, and maintainable code.
*   Maintain backward compatibility.
*   When writing documentation, do not document non-existent features. Use `### TODO` to mark areas for future implementation.
