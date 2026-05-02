# Security and integrity

apidoc-gen is designed for use in codebases that contain sensitive or proprietary information. This document explains how the tool handles your code and data, and how to report security issues.

## Your code stays on your machine

- **Local-only by default** – The CLI runs entirely on your machine and reads only your source code. It does not send your source code or configuration to any remote service.
- **No telemetry or analytics** – The application does not collect usage data, report errors to external servers, or phone home.
- **Opt-in network calls** – The tool only contacts the network when you explicitly trigger a feature that needs it:
  - **Postman upload** (only when `--type=postman`) – If you provide a Postman API key (interactively, via `--postman-api-key`, or via the env vars `APIDOC_POSTMAN_API_KEY` / `POSTMAN_API_KEY`), the generated `collection.json` is uploaded to `https://api.getpostman.com/collections` so you can view it in the Postman UI. Skip with `--no-upload`. In `--no-interactive` mode the upload is silently skipped if no key is configured (no prompt, no surprise calls). See **[Postman credentials](#postman-credentials)** below.
  - **Docusaurus init** (only when `--type=custom`) – Runs `npx create-docusaurus@latest`, which downloads packages from the public npm registry.
  - **Swagger UI HTML** (only when `--type=swagger`) – The generated `index.html` references `cdn.jsdelivr.net/npm/swagger-ui-dist@5/...` for the JS/CSS at *view time* (when you open the HTML). Nothing is fetched at generation time.

  In all three cases your **source code is never transmitted**. The Postman upload sends only the generated collection JSON (route paths, parameters, request/response shapes, descriptions you wrote).

## What the tool reads and writes

- **Reads** – The tool reads only what is necessary to generate documentation:
  - Your project directory (path you pass or current directory)
  - `go.mod` (to detect framework)
  - `.go` files (to find route definitions and handler comments)
  - Optional config file `.apidoc-gen.yaml` in the project
- **Skips** – By default it skips directories such as `vendor`, `node_modules`, `.git`, and test directories. You can override exclusions via config or `--exclude`.
- **Writes** – It writes only to the output directory you specify (default `./docs`): OpenAPI/Swagger files, Postman collection, or Docusaurus site files. It does not modify your source code.

## Configuration and secrets

- **Config file** – `.apidoc-gen.yaml` may contain paths, titles, and server URLs. Do not put secrets (API keys, passwords, tokens) in this file. The tool does not read secrets from it.
- **Environment variables** – Most `APIDOC_*` env vars are non-sensitive configuration (e.g. output path, doc type). The exceptions are `APIDOC_POSTMAN_API_KEY` and the Postman-standard `POSTMAN_API_KEY`, which carry your Postman API key for the optional upload step (see below).
- **Generated docs** – Generated documentation may reflect route paths and (if you added them) descriptions. Review generated output before publishing; avoid committing secrets into docs.

### Postman credentials

The Postman upload feature uses an **API key** (Postman does not expose OAuth / username-password login for third-party CLIs).

- **Where the key is stored** – If you enter your key at the interactive prompt, it is written to `~/.config/apidoc-gen/credentials.json` with file mode `0600` and the parent directory created with mode `0700`. The credentials file is **never** read for anything other than the Postman API call.
- **Lookup order at runtime** – `--postman-api-key` flag → `APIDOC_POSTMAN_API_KEY` → `POSTMAN_API_KEY` → credentials file → interactive prompt (only when in interactive mode).
- **What is sent** – Only the generated Postman collection JSON, sent to `https://api.getpostman.com/collections` (POST first time, PUT on subsequent runs once the UID is cached locally) with header `X-Api-Key: <your-key>`. No source code, no environment, no other files.
- **Local cache** – After a successful upload, the returned collection UID is stored in `.apidoc-gen-cache.json` in your project directory so future runs update the same collection instead of creating duplicates. The cache file contains no secrets — only `{ "collections": { "<title>": "<uid>" } }`. It is in `.gitignore` by default.
- **To revoke** – Delete `~/.config/apidoc-gen/credentials.json`, and revoke the key in your Postman account settings (<https://postman.co/settings/me/api-keys>).
- **To suppress uploads entirely** – Pass `--no-upload`, or simply do not provide a key (the tool silently skips the upload step in `--no-interactive` mode).

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
