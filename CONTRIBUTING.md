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
2. **Prerequisites:** Go 1.24 or later.
3. **Build and test:**
   ```bash
   cd apidoc-gen
   make build
   make test
   ```
4. **Run locally:** `./bin/apidoc-gen generate --help` or `make run` (runs generate in the project directory). Build outputs are written to `./bin/`.

## Project layout

- `cmd/` – CLI commands (Cobra), flags, and the generate/init run logic.
- `pkg/analyzer/` – Code analysis (framework detection, route/schema parsing).
- `pkg/generator/` – Output generators (Swagger/OpenAPI, Postman).
- `pkg/models/` – Shared data structures.
- `pkg/config/` – Configuration and validation.
- `pkg/postman/` – Postman API client and credentials/cache management.
- `internal/prompt/` – Interactive prompts.
- `internal/annotations/` – `--write-annotations`: writes swag comment blocks to handler source.
- `docs/` – User docs (CONFIGURATION, TROUBLESHOOTING).
- `web/` – Landing/docs pages (`index.html`, `docs.html`) — not part of the CLI itself.
- `examples/` – Sample Go APIs (Gin, Chi, Gorilla Mux) for testing the generator end to end.

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
