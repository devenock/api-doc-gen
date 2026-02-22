# Security and integrity

apidoc-gen is designed for use in codebases that contain sensitive or proprietary information. This document explains how the tool handles your code and data, and how to report security issues.

## Your code stays on your machine

- **Local-only execution** – The CLI runs entirely on your machine. It does not send your source code, configuration, or generated documentation to any remote service.
- **No telemetry or analytics** – The application does not collect usage data, report errors to external servers, or phone home.
- **No network access to your code** – Other than optional steps (e.g. `npx create-docusaurus` for the Docusaurus output, which fetches public npm packages), the tool does not transmit your project contents over the network.

## What the tool reads and writes

- **Reads** – The tool reads only what is necessary to generate documentation:
  - Your project directory (path you pass or current directory)
  - `go.mod` (to detect framework)
  - `.go` files (to find route definitions and handler comments)
  - Optional config file `.apidoc-gen.yaml` in the project
- **Skips** – By default it skips directories such as `vendor`, `node_modules`, `.git`, and test directories. You can override exclusions via config or `--exclude`.
- **Writes** – It writes only to the output directory you specify (default `./docs`): OpenAPI/Swagger files, Postman collection, or Docusaurus site files. It does not modify your source code.

## Configuration and secrets

- **Config file** – `.apidoc-gen.yaml` may contain paths, titles, and server URLs. Do not put secrets (API keys, passwords, tokens) in this file. The tool does not use it for any network authentication.
- **Environment variables** – `APIDOC_*` env vars are used only for configuration (e.g. output path, doc type). Do not use them to pass secrets.
- **Generated docs** – Generated documentation may reflect route paths and (if you added them) descriptions. Review generated output before publishing; avoid committing secrets into docs.

## Dependency and supply-chain hygiene

- **Go modules** – Dependencies are declared in `go.mod` and `go.sum`. Use `go mod verify` and keep dependencies updated.
- **Docusaurus (optional)** – The “custom” output runs `npx create-docusaurus@latest`, which installs packages from the npm registry. Use only in environments where you accept that dependency chain.

## Reporting a vulnerability

If you believe you have found a security vulnerability in apidoc-gen:

1. **Do not** open a public issue for sensitive findings.
2. Report it privately (e.g. to the maintainers via the repository’s preferred contact or security policy).
3. Include a clear description, steps to reproduce, and impact if possible.
4. Allow a reasonable time for a fix before any public disclosure.

We take reports seriously and will respond as promptly as we can.

## Integrity of the application

- The tool is open source so you can inspect and build from source.
- Building from source: `go build -o apidoc-gen .` (see [README](README.md#installation)).
- Prefer official releases or building from a tagged version when possible.
- If you use a pre-built binary, obtain it from a trusted source and verify checksums when published.
