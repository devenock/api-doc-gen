# api-doc-gen

[![CI](https://github.com/devenock/api-doc-gen/actions/workflows/ci.yml/badge.svg)](https://github.com/devenock/api-doc-gen/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/devenock/api-doc-gen.svg)](https://pkg.go.dev/github.com/devenock/api-doc-gen)
[![Go Report Card](https://goreportcard.com/badge/github.com/devenock/api-doc-gen)](https://goreportcard.com/report/github.com/devenock/api-doc-gen)
[![Latest release](https://img.shields.io/github/v/release/devenock/api-doc-gen)](https://github.com/devenock/api-doc-gen/releases)
[![License](https://img.shields.io/github/license/devenock/api-doc-gen)](LICENSE)

CLI that scans your Go API and generates **Swagger/OpenAPI** or a **Postman Collection** — no annotations required.

Supports **Gin, Echo, Fiber, Gorilla Mux, Chi** (auto-detected).

---

## Why

The standard way to generate OpenAPI docs for a Go API — [`swaggo/swag`](https://github.com/swaggo/swag) — works by parsing comment annotations you write above every handler:

```go
// @Summary      Show an account
// @Router       /accounts/{id} [get]
// @Param        id   path  int  true  "Account ID"
// @Success      200  {object}  model.Account
func (c *Controller) ShowAccount(ctx *gin.Context) {
```

That's a real, well-established tool, but the annotations are a second source of truth you maintain by hand: add an endpoint and forget the comment block, and the docs silently fall behind the code with no error to catch it.

**api-doc-gen removes that step entirely.** It reads your code as-is — route registrations, handler signatures, request/response structs, binding calls — via Go's own AST parser, the same way `go vet` or `gofmt` do, and builds the OpenAPI/Postman spec from what's actually there. No comments to write, no existing code to touch.

One consequence of that: because there's nothing to keep in sync, docs stay accurate as you build incrementally. Register one handler, generate, ship; add the next one whenever it's ready, generate again — each run reflects exactly what's in the code at that moment, with zero extra steps. (To be precise: every run is a full, fresh scan of the project, not a cached diff of what changed — "incremental" describes the workflow this enables, not an incremental-build engine under the hood.)

"No annotations" only helps if the scanner is actually reliable, so the analyzer is built to degrade honestly rather than guess silently: it resolves route groups, auth middleware, embedded struct fields, query/path parameters, and response bodies across real-world patterns in Gin, Echo, Fiber, Gorilla Mux, and Chi; falls back to generic `net/http`-style detection for anything else; and surfaces ambiguity instead of hiding it — a `.go` file that fails to parse, a framework it can't confidently pick between, or zero endpoints found are all reported explicitly (`-v` for detail) rather than producing a spec that looks complete but silently isn't.

---

## Install

Requires Go 1.26.8 or later.

```bash
go install github.com/devenock/api-doc-gen@latest
```

The binary lands in `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure that directory is on your `PATH`.

Prebuilt binaries for Linux, macOS, and Windows (amd64/arm64) are also published on the
[Releases page](https://github.com/devenock/api-doc-gen/releases) for each tagged version.

---

## Generate docs for your project

Run from your Go project root (the directory that has `go.mod`):

```bash
api-doc-gen generate
```

This starts an interactive wizard — choose your doc type, output folder, title, and so on.

**Skip the wizard** (CI, scripts, or if you already know what you want):

```bash
api-doc-gen generate --no-interactive --type swagger -o ./docs
api-doc-gen generate --no-interactive --type postman -o ./docs
```

**Point at a project in another directory:**

```bash
api-doc-gen generate /path/to/your-api \
  --no-interactive --type swagger \
  -o /path/to/your-api/docs
```

---

## Output

| Type | Files created |
|------|--------------|
| `swagger` | `openapi.json`, `openapi.yaml`, `index.html` (Swagger UI) |
| `postman` | `collection.json` (Postman Collection v2.1) |

**Swagger** — the Swagger UI opens in your browser automatically after generation. To reopen it later, open `./docs/index.html` directly in your browser.

**Postman** — open Postman, click **Import** in the sidebar, then drag `collection.json` onto the dialog.

---

## Coverage

What the analyzer actually detects and emits, checked against the code rather than aspirational:

- [x] Paths, operations, path and query parameters
- [x] Request bodies — JSON binding calls (`ShouldBindJSON`, `BodyParser`, `Decode`, and common project-specific wrapper names matched by hint)
- [x] Response bodies — JSON responses across Gin/Echo/Fiber/Gorilla Mux/Chi/net/http response patterns
- [x] Bearer auth detection — marks a route `security: [BearerAuth]` when its middleware looks like auth (or matches your configured `auth_middleware` list)
- [x] Struct tags — `json` (name, `omitempty`, `-` to exclude), `binding:"required"` / `validate:"required"`
- [x] Embedded struct fields promoted into the parent schema
- [x] Tags for Swagger UI grouping, derived from the path
- [x] OpenAPI 3.0.3 and Postman Collection v2.1 output
- [ ] Header parameters — not extracted
- [ ] Non-JSON content types — request/response bodies are always modeled as `application/json`; no multipart/form-data, XML, or plain text
- [ ] Auth schemes other than Bearer — no API Key, Basic Auth, or OAuth2 security scheme detection
- [ ] Enums, example values, response headers — the schema model has fields for all three (so output stays forward-compatible), but nothing in the analyzer populates them yet
- [ ] Types from outside the scanned project — `time.Time` is special-cased; any other external type (`uuid.UUID`, a shared internal module, etc.) resolves to an empty `object` with no fields
- [ ] File uploads (`multipart/form-data`) — not detected

Found a pattern that isn't covered? [Open an issue](https://github.com/devenock/api-doc-gen/issues) — new patterns belong in the analyzer, not worked around.

---

## Applying it to your Go project

Your project does not need any changes — the tool reads your existing route definitions.

**Recommended layout:**

```
my-go-api/
├── go.mod
├── main.go
├── handlers/
├── .apidoc-gen.yaml   ← optional config (see below)
└── docs/              ← generated output
```

**Optional `Makefile` target** so the whole team runs the same command:

```makefile
docs:
	api-doc-gen generate --no-interactive --type swagger -o ./docs
```

**Optional config file** — run `api-doc-gen init` inside your project to create `.apidoc-gen.yaml`. Commit it so everyone shares the same defaults (output dir, title, framework, etc.). In interactive mode, doc type and framework are skipped entirely once set in the file; title, version, base path, and output directory are still prompted but pre-filled with the file's value — press Enter to accept it. Use `--no-interactive` to skip all prompts and use the file (plus flags/env) as-is.

---

## Key flags

| Flag | What it does |
|------|-------------|
| `-t, --type` | `swagger` or `postman` |
| `-o, --output` | Output directory (default `./docs`) |
| `-f, --framework` | Force framework: `gin` `echo` `fiber` `gorilla` `chi` |
| `-y, --no-interactive` | No prompts — required for CI |
| `--dry-run` | Show what would be generated without writing files |
| `--upload` | Upload Postman collection via Postman API (prompts for API key once) |
| `--skip-build-check` | Skip the `go vet ./...` pre-flight check (see below) |
| `--required-by-default` | Mark every struct field required unless it has `json:",omitempty"` (default: only `binding`/`validate:"required"` tags count) |
| `--tags` | Filter endpoints by tag: `users` includes only that tag, `!internal` excludes it (comma-separated, mixable) |

Full reference: `api-doc-gen generate --help`

---

## Build check

Before analyzing, `generate` runs `go vet ./...` against the target project as a pre-flight check. It's purely advisory — a project that fails it still gets docs generated from whatever the AST parser can recover, but you get a warning up front that the result may be incomplete, rather than silently missing routes with no explanation:

```
⚠️  This project does not currently pass `go vet ./...` —
   generated docs may be incomplete or incorrect.
   Run with -v for details, or pass --skip-build-check to suppress this check.
```

Pass `--skip-build-check` to disable it (e.g. running against a branch mid-refactor in CI). It also skips itself automatically wherever the `go` toolchain isn't on `PATH` — including this project's own Docker runtime image, which ships as a slim `alpine` base with just the compiled binary and no Go toolchain.

---

## Postman upload (optional)

If you want the collection to appear in Postman automatically without manual import:

1. Generate a free API key at <https://postman.co/settings/me/api-keys>
2. Run with `--upload`:

```bash
api-doc-gen generate --no-interactive --type postman --upload
```

You will be prompted for the key once. It is saved to `~/.config/apidoc-gen/credentials.json` and reused on future runs. Repeat runs update the same collection — no duplicates.

For CI, export the key as an env variable:

```bash
export APIDOC_POSTMAN_API_KEY=your_key
api-doc-gen generate -y --type postman --upload
```

---

## Docker

No Go installation needed. Pull the published image:

```bash
docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace \
  ghcr.io/devenock/api-doc-gen:latest generate --no-interactive --type swagger -o ./docs
```

Or build it yourself from source:

```bash
docker build -t api-doc-gen .
docker run --rm \
  -v "$(pwd)":/workspace \
  -w /workspace \
  api-doc-gen generate --no-interactive --type swagger -o ./docs
```

The container runs as a non-root user. If the generated files come out owned by
a UID your host user can't write to, add `--user "$(id -u):$(id -g)"` to the
`docker run` command above.

---

## Troubleshooting

**No endpoints found** — pass `--framework` explicitly or run with `-v` (verbose) to see what the analyzer is reading.

**Config not picked up** — ensure `.apidoc-gen.yaml` is in the directory you are running the command from, or run `--show-config` to inspect the effective config.

**Prompts appearing in CI** — always pass `-y` (`--no-interactive`) and `--type` in CI pipelines.

Full troubleshooting guide: [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). MIT licensed.
