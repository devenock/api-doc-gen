# api-doc-gen

CLI that scans your Go API and generates **Swagger/OpenAPI** or a **Postman Collection** — no annotations required.

Supports **Gin, Echo, Fiber, Gorilla Mux, Chi** (auto-detected).

---

## Install

```bash
go install github.com/devenock/api-doc-gen@latest
```

The binary lands in `$(go env GOPATH)/bin` (usually `~/go/bin`). Make sure that directory is on your `PATH`.

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
api-doc-gen generate /path/to/your-api --no-interactive --type swagger -o /path/to/your-api/docs
```

---

## Output

| Type | Files created |
|------|--------------|
| `swagger` | `openapi.json`, `openapi.yaml`, `index.html` (Swagger UI) |
| `postman` | `collection.json` (Postman Collection v2.1) |

**Swagger** — run `api-doc-gen serve ./docs` to open the Swagger UI in your browser automatically.

**Postman** — open Postman, click **Import** in the sidebar, then drag `collection.json` onto the dialog.

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

**Optional config file** — run `api-doc-gen init` inside your project to create `.apidoc-gen.yaml`. Commit it so everyone shares the same defaults (output dir, title, framework, etc.). You will not be prompted again for things already in the file.

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

Full reference: `api-doc-gen generate --help`

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

No Go installation needed:

```bash
docker build -t apidoc-gen .
docker run --rm -v "$(pwd)":/workspace -w /workspace apidoc-gen generate --no-interactive --type swagger -o ./docs
```

---

## Troubleshooting

**No endpoints found** — pass `--framework` explicitly or run with `-v` (verbose) to see what the analyzer is reading.

**Config not picked up** — ensure `.apidoc-gen.yaml` is in the directory you are running the command from, or run `--show-config` to inspect the effective config.

**Prompts appearing in CI** — always pass `-y` (`--no-interactive`) and `--type` in CI pipelines.

Full troubleshooting guide: [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md). MIT licensed.
