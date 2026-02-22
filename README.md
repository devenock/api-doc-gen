# API Documentation Generator (apidoc-gen)

CLI that scans your Go API codebase and generates API documentation as **Swagger/OpenAPI**, **Postman**, or a **Docusaurus** site.

- **Install:** [Binary](#installation) · [Docker](#docker)
- **Get started:** [Quick start](#quick-start) below, or open **[web/index.html](web/index.html)** for a single-page guide with copy-paste commands.

## Features

- Scans Go projects and detects **Gin, Echo, Fiber, Gorilla Mux, Chi**
- Outputs **Swagger/OpenAPI 3.0** (JSON, YAML, Swagger UI), **Postman Collection v2.1**, or **custom Docusaurus** site
- Config via **file** (`.apidoc-gen.yaml`), **env** (`APIDOC_*`), or **flags**
- **Interactive** prompts or **non-interactive** for CI (`--no-interactive`, `-y`)

## Installation

**Prerequisites:** Go 1.22+ (Node.js/npm only for Docusaurus output)

```bash
# Install binary
go install github.com/devenock/api-doc-gen@latest

# Or build from source
git clone https://github.com/yourusername/apidoc-gen.git && cd apidoc-gen
go build -o apidoc-gen
```

## Quick start

From your Go API project root:

```bash
apidoc-gen init                    # optional: create .apidoc-gen.yaml
apidoc-gen generate               # interactive: choose type, framework, etc.
# or
apidoc-gen generate --no-interactive --type swagger -o ./docs
```

Then open `docs/index.html` (Swagger) or import `docs/collection.json` (Postman).

## Docker

Build and run without installing Go:

```bash
docker build -t apidoc-gen .
docker run --rm -v "$(pwd)":/workspace -w /workspace apidoc-gen generate --no-interactive --type swagger -o ./docs
```

Use your API project path instead of `$(pwd)` if you’re not in the project root.

## Usage

### Commands

| Command | Description |
|--------|-------------|
| `generate [path]` | Generate docs (path defaults to `.`) |
| `init` | Create `.apidoc-gen.yaml` in current directory |
| `completion <shell>` | Shell completion (bash, zsh, fish) |

### Main flags (generate)

| Flag | Description |
|------|-------------|
| `-t, --type` | `swagger` \| `postman` \| `custom` |
| `-o, --output` | Output directory (default `./docs`) |
| `-f, --framework` | `gin` \| `echo` \| `fiber` \| `gorilla` \| `chi` (or empty = auto) |
| `-y, --no-interactive` | No prompts (for CI/scripts) |
| `-q, --quiet` | Suppress progress output |
| `-v, --verbose` | Verbose output |
| `--dry-run` | Show what would be generated, no files written |
| `--show-config` | Print effective config and exit |

Run `apidoc-gen generate --help` for the full list.

### Configuration

- **Config file:** `.apidoc-gen.yaml` in project root (create with `apidoc-gen init`).
- **Env:** `APIDOC_TYPE`, `APIDOC_OUTPUT`, `APIDOC_FRAMEWORK`, etc.
- **Precedence:** config file → env → flags.

Full reference: [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

### Automation / CI

Use `--no-interactive` (or `-y`) and set `--type` so the CLI doesn’t wait for input.

- **Exit codes:** `0` success, `1` validation error, `2` runtime error.

```bash
apidoc-gen generate -y --type swagger -o ./docs --title "My API"
```

### Shell completion

```bash
source <(apidoc-gen completion bash)   # or zsh
apidoc-gen completion fish | source   # fish
```

## Supported frameworks

Gin, Echo, Fiber, Gorilla Mux, Chi (auto-detected from `go.mod`), plus generic route patterns.

## Output formats

| Type | Output |
|------|--------|
| **swagger** | `openapi.json`, `openapi.yaml`, `index.html` (Swagger UI) |
| **postman** | `collection.json` (Postman Collection v2.1) |
| **custom** | Docusaurus site in output dir (`npm start` to run) |

## Best practices

1. **Comments** – Plain Go doc comments on handlers become summary/description. Path params are inferred from routes (e.g. `/users/:id`).
2. **Config file** – Use `.apidoc-gen.yaml` for consistent defaults across the team.
3. **Exclude dirs** – Default excludes: `vendor`, `node_modules`, `.git`, `test`, `tests`. Override with `exclude` in config or `--exclude`.

## Project structure

```
├── cmd/root.go
├── pkg/analyzer/   # Code analysis
├── pkg/generator/  # Swagger, Postman, Docusaurus
├── pkg/models/     # Data models
├── pkg/config/     # Configuration
├── internal/prompt/
├── docs/           # CONFIGURATION.md, TROUBLESHOOTING.md
├── web/            # Getting-started UI (index.html)
├── Dockerfile
├── Makefile
└── README.md
```

## Development

```bash
make build   # build binary
make test    # run tests
make run     # build and run generate
make install # install locally
```

## Troubleshooting

- **No endpoints** – Set `--framework` or use `-v`. See [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md).
- **Config not read** – Ensure `.apidoc-gen.yaml` is in cwd or use `--config`. Run `--show-config` to inspect.
- **Prompts in CI** – Use `-y` and `--type` (and other flags) so the run is non-interactive.
- **npx not found** (Docusaurus) – Install Node.js/npm and ensure `npx` is on PATH.

## License

MIT.
