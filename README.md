# API Documentation Generator (apidoc-gen)

CLI that scans your Go API codebase and generates API documentation as **Swagger/OpenAPI**, **Postman**, or a **Docusaurus** site.

- **Install:** [Binary](#installation) · [Docker](#docker)
- **Get started:** [Quick start](#quick-start) below, or open **[web/index.html](web/index.html)** for a single-page guide.
- **Use in your backend:** [Using in your backend codebase](#using-in-your-backend-codebase) (where to run, what it reads, optional Make target).

## Features

- Scans Go projects and detects **Gin, Echo, Fiber, Gorilla Mux, Chi**
- Outputs **Swagger/OpenAPI 3.0** (JSON, YAML, Swagger UI), **Postman Collection v2.1**, or **custom Docusaurus** site
- Config via **file** (`.apidoc-gen.yaml`), **env** (`APIDOC_*`), or **flags**
- **Interactive** prompts or **non-interactive** for CI (`--no-interactive`, `-y`)

## Installation

**Prerequisites:** Go 1.22+ (Node.js/npm only for Docusaurus output)

**Recommended (latest code):** Clone this repo and install the CLI into your Go bin directory. Then you can run `api-doc-gen` from any project (including your backend).

```bash
git clone https://github.com/devenock/api-doc-gen.git
cd api-doc-gen
go install .
```

Ensure `$(go env GOPATH)/bin` (or `$HOME/go/bin`) is in your PATH. Then from any directory, including your backend project, run `api-doc-gen generate ...`.

**Alternative:** Install from the module (use a tagged release when available for latest fixes):

```bash
go install github.com/devenock/api-doc-gen@latest
```

**Command name:** The binary is **`api-doc-gen`** (with a hyphen). Use `api-doc-gen init`, `api-doc-gen generate`, etc.

If the command is not found, see [Command not found](docs/TROUBLESHOOTING.md#command-not-found-after-go-install).

## Quick start

From your Go API project root:

```bash
api-doc-gen init                    # optional: create .apidoc-gen.yaml
api-doc-gen generate               # interactive: choose type, framework, etc.
# or
api-doc-gen generate --no-interactive --type swagger -o ./docs
```

Then open `docs/index.html` (Swagger) or import `docs/collection.json` (Postman).

## Using in your backend codebase

You run apidoc-gen **from your Go API project** (or point it at that directory). It reads your repo and writes docs into a folder you choose.

1. **Where to run** – From the root of your backend repo (the directory that has `go.mod` and your route definitions). The tool uses the current directory if you don’t pass a path.
2. **What it reads** – `go.mod` (to detect framework) and `.go` files under that path. It skips `vendor`, `node_modules`, `.git`, and test dirs by default.
3. **Where output goes** – By default `./docs` in the directory you ran the command from; override with `-o` (e.g. `-o ./api-docs`).
4. **Optional config in repo** – Run `apidoc-gen init` inside your backend repo to create `.apidoc-gen.yaml` there. Commit it so the team shares the same defaults (output dir, type, title, etc.).
5. **From another directory** – To generate docs for a backend that isn’t your cwd:  
   `apidoc-gen generate /path/to/your-api --no-interactive --type swagger -o /path/to/your-api/docs`

**Example: backend repo layout**

```
my-go-api/
├── go.mod
├── main.go
├── handlers/
├── .apidoc-gen.yaml   # optional, from apidoc-gen init
└── docs/              # generated (e.g. openapi.json, index.html)
```

**Optional Make target** (in your backend’s `Makefile`):

```makefile
docs:
	apidoc-gen generate --no-interactive --type swagger -o ./docs
```

Then run `make docs` from your backend root.

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

## Security and privacy

apidoc-gen runs entirely on your machine and does not send your code or data anywhere. For handling of sensitive codebases, dependency hygiene, and how to report vulnerabilities, see **[SECURITY.md](SECURITY.md)**.

## Contributing

Contributions are welcome. Please read **[CONTRIBUTING.md](CONTRIBUTING.md)** for setup and pull request guidelines. By participating, you agree to our **[Code of Conduct](CODE_OF_CONDUCT.md)**.

## License

MIT.
