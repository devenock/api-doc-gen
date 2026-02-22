# Contributing to apidoc-gen

Thank you for considering contributing. This document explains how to get set up and submit changes.

## Code of conduct

By participating, you agree to uphold our [Code of Conduct](CODE_OF_CONDUCT.md).

## How to contribute

- **Bug reports and feature requests** – Open an issue. Describe the problem or idea and, for bugs, include steps to reproduce and your environment (Go version, OS).
- **Documentation** – Fixes and improvements to README, docs, or the web UI are welcome. Open an issue or a pull request.
- **Code** – Follow the steps below to propose a change.

## Development setup

1. **Fork and clone** the repository.
2. **Prerequisites:** Go 1.22 or later.
3. **Build and test:**
   ```bash
   cd apidoc-gen
   make build
   make test
   ```
4. **Run locally:** `./apidoc-gen generate --help` or `make run` (runs generate in the project directory).

## Project layout

- `cmd/` – CLI commands (Cobra).
- `pkg/analyzer/` – Code analysis (framework detection, route parsing).
- `pkg/generator/` – Output generators (Swagger, Postman, Docusaurus).
- `pkg/models/` – Shared data structures.
- `pkg/config/` – Configuration and validation.
- `internal/prompt/` – Interactive prompts.
- `docs/` – User docs (CONFIGURATION, TROUBLESHOOTING).
- `web/` – Getting-started UI (`index.html`).

## Submitting changes

1. **Create a branch** from `main` (e.g. `feature/add-xyz` or `fix/issue-123`).
2. **Make your changes** – Keep commits focused and messages clear.
3. **Run tests:** `make test`. Ensure the project still builds: `make build`.
4. **Open a pull request** against `main`. Describe what you changed and why; reference any issue.
5. **Review** – Address feedback. Maintainers may request edits before merging.

## Code and style

- Write clear, idiomatic Go. Follow standard formatting (`gofmt` / `goimports`).
- New public APIs should have a short doc comment.
- Prefer small, reviewable PRs. For large features, consider opening an issue first to discuss.

## Questions

If something is unclear, open an issue with the question so others can benefit from the answer.
