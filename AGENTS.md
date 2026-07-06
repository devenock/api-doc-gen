# AGENTS.md

Guidance for AI coding agents (Cursor, Claude Code, etc.) working in this repository. Read this **before** making changes.

This file is generated from a scan of the codebase (Go source under `cmd/`, `pkg/`, `internal/`), the project documentation (`README.md`, `docs/*`, `CONTRIBUTING.md`, `SECURITY.md`, `CODE_OF_CONDUCT.md`), build/runtime files (`Makefile`, `Dockerfile`, `go.mod`, `.gitignore`, `.dockerignore`), and the static landing page in `web/`. Every claim below maps to something concretely present in the repo at the time of writing.

---

## 1. What this project is

`api-doc-gen` (also referred to as **`apidoc-gen`** in some places — see §11) is a **command-line tool, written in Go**, that **scans a Go HTTP API codebase and generates API documentation** in one of three output formats:

| Output type | Files produced (in the chosen output dir) |
|-------------|--------------------------------------------|
| `swagger` (default suggested) | `openapi.yaml`, `openapi.json`, `index.html` (Swagger UI loaded from `cdn.jsdelivr.net/npm/swagger-ui-dist@5`) |
| `postman` | `collection.json` (Postman Collection v2.1) |
| `custom`  | A Docusaurus site (`package.json`, `docusaurus.config.js`, `sidebars.js`, `docs/*.md`, `src/css/custom.css`, etc.) |

The tool works by **statically parsing Go source** with `go/ast` + `go/parser` — there is no runtime instrumentation, no LLM call, and no network request to your code. The auto-detected/explicit framework decides which route-extraction strategy is used. See `docs/RICH_DOCS_DESIGN.md` for the longer design intent (Phase 1 static analysis is "Done"; Phase 2 optional AI enrichment is **not** implemented).

### Supported source frameworks (auto-detected from `go.mod`)

`pkg/analyzer/analyzer.go` (`DetectFramework`) recognizes substrings in `go.mod`:

| Framework | Substring matched in `go.mod` | Internal id (`models.FrameWorkType`) |
|-----------|-------------------------------|---------------------------------------|
| Gin       | `github.com/gin-gonic/gin`    | `gin`     |
| Echo      | `github.com/labstack/echo`    | `echo`    |
| Fiber     | `github.com/gofiber/fiber`    | `fiber`   |
| Gorilla Mux | `github.com/gorilla/mux`    | `gorilla` |
| Chi       | `github.com/go-chi/chi`       | `chi`     |
| (none)    | — falls back to "generic" `MethodName(path, handler)` pattern matcher | `unknown` |

> Implementation note: `Echo` and `Fiber` route parsing delegates to `parseGinRoutes` because the call signatures are similar enough (`r.METHOD("path", handler)`). **`Chi` now has its own dedicated recursive walker (`walkChiStmts`)** that handles nested `r.Route(path, fn)` / `r.Group(fn)` closures with correct prefix accumulation and `Use(authMiddleware)` inheritance at every nesting level (see `parseFile`).

---

## 2. Repository layout

```
.
├── AGENTS.md                  # ← this file
├── README.md                  # User-facing intro
├── CONTRIBUTING.md            # Dev setup + PR workflow
├── CODE_OF_CONDUCT.md         # Contributor Covenant 2.1
├── SECURITY.md                # Privacy/security posture, vuln reporting
├── LICENSE                    # MIT
├── Makefile                   # build / test / run / install / clean
├── Dockerfile                 # Multi-stage build (golang:1.24-alpine -> alpine:3.19)
├── .dockerignore              # Excludes md/docs/web/yaml from image context
├── .gitignore
├── go.mod                     # Module: github.com/devenock/api-doc-gen, go 1.24.3
├── go.sum
├── main.go                    # 7-line entry point: cmd.Execute()
│
├── cmd/
│   └── root.go                # Cobra commands, flag binding, viper init, run loops, exit codes
│
├── pkg/
│   ├── analyzer/
│   │   └── analyzer.go        # ~1050 LoC. Two-pass AST scan, framework detection, route+type extraction
│   ├── generator/
│   │   ├── generator.go       # Generator interface + factory (NewGenerator)
│   │   ├── swagger.go         # OpenAPI 3.0.3 YAML/JSON + Swagger UI HTML
│   │   ├── postman.go         # Postman Collection v2.1
│   │   └── custom.go          # Docusaurus site (uses `npx create-docusaurus@latest` or fallback)
│   ├── postman/
│   │   ├── client.go          # HTTP client for api.getpostman.com (Me, CreateCollection, UpdateCollection)
│   │   ├── auth.go            # Credentials file (~/.config/apidoc-gen/credentials.json, 0600) + .apidoc-gen-cache.json UID cache
│   │   └── desktop.go         # Postman desktop detection (IsDesktopInstalled) + launch via postman:// scheme (OpenDesktop)
│   ├── models/
│   │   └── models.go          # APISpec, Endpoint, Schema, Parameter, Response, etc.
│   └── config/
│       └── config.go          # Config struct + Validate + ShouldExclude
│
├── internal/
│   ├── prompt/
│   │   └── prompt.go          # promptui-based interactive wizard (GetUserPreferences)
│   └── annotations/
│       └── writer.go          # `--write-annotations`: writes swag-style `// @Summary ...` blocks above handlers
│
├── docs/                      # USER docs (do NOT confuse with generated output dir, also default-named ./docs)
│   ├── CONFIGURATION.md       # Flag/env/config-key precedence reference
│   ├── TROUBLESHOOTING.md     # PATH issues, no-endpoints, npx-not-found, etc.
│   └── RICH_DOCS_DESIGN.md    # Design doc & roadmap (Phase 1 done, Phase 2 = optional AI)
│
└── web/
    └── index.html             # Standalone single-page getting-started UI (vanilla HTML/CSS/JS, dark/light theme)
```

> **Naming gotcha:** `./docs/` in this repo holds **project documentation**, but the CLI's *default output directory* is also called `./docs`. When you (or a user) run `api-doc-gen generate` inside this repo, the generator will try to write into `./docs` and **collide with the existing project docs**. Always pass `-o` to a different directory when dog-fooding. The `.gitignore` reserves `docs-out/` and `docs-q/` for that purpose.

---

## 3. Tech stack and dependencies

- **Language:** Go. `go.mod` declares `go 1.24.3`. `README.md` says **"Go 1.24+"**, `Dockerfile` builds on `golang:1.24-alpine`, and `CONTRIBUTING.md` says **"Go 1.24 or later"** — all four are now consistent. If you bump or constrain Go versions, update **all four** locations.
- **CLI framework:** `github.com/spf13/cobra` v1.10.1
- **Configuration:** `github.com/spf13/viper` v1.21.0 (file + env + flags)
- **Interactive prompts:** `github.com/manifoldco/promptui` v0.9.0
- **YAML output:** `gopkg.in/yaml.v3` v3.0.1
- **Standard library only** for AST parsing (`go/ast`, `go/parser`, `go/token`), HTTP file serving (`net/http`), filepath walking, etc.

> **`go.mod` note:** Every `require` line is currently marked `// indirect`. If you add direct usage of any of these packages from a new file, run `go mod tidy` so the markers are refreshed.

---

## 4. Building, running, testing

The canonical entry points are the `Makefile` targets:

```bash
make build      # mkdir -p bin && go build -o bin/apidoc-gen .
make test       # go test ./...   ← currently no _test.go files exist; this is a no-op
make run        # build, then ./bin/apidoc-gen generate
make install    # go install . (installs to $GOPATH/bin)
make clean      # rm -rf bin
```

**Build-output convention (project rule): all build artifacts go into `./bin/`. Never write a binary to the repo root.** `bin/` is in `.gitignore` and `.dockerignore`. The `Makefile` is the single source of truth for this — if you add new build steps (e.g. cross-compile targets, race-detector builds, fuzz binaries), output them under `bin/` too (e.g. `bin/apidoc-gen-linux-amd64`).

Things to be aware of:

- **`make build` produces `./bin/apidoc-gen`** (no hyphen). But `go install github.com/devenock/api-doc-gen@latest` installs **`api-doc-gen`** (with hyphen, taken from the module path's last segment) into `$GOPATH/bin`. So users see `api-doc-gen` and devs building from source see `bin/apidoc-gen`. The Cobra `Use:` field is `"apidoc-gen"` (`cmd/root.go`). Don't "fix" any one of these without considering the other two.
- **There are no tests.** `make test` runs `go test ./...` which currently exits with `?` for every package. Don't claim coverage; if you add tests, put them in standard `_test.go` files next to the code they exercise.
- The legacy pre-built `api-doc-gen` (~12 MB, mach-o arm64) that used to live at the repo root has been moved to `./bin/api-doc-gen` and removed from git tracking (`git rm --cached`). It still exists on disk as a local build artifact. New work should never add binaries outside `bin/`.

### Docker

```bash
docker build -t apidoc-gen .
docker run --rm -v "$(pwd)":/workspace -w /workspace apidoc-gen \
  generate --no-interactive --type swagger -o ./docs
```

The `Dockerfile`:
1. **Build stage** — `golang:1.24-alpine`, `go mod download`, then `CGO_ENABLED=0 go build -ldflags="-s -w" -o /apidoc-gen .`.
2. **Runtime** — `alpine:3.19` + `ca-certificates`, copies binary to `/usr/local/bin/apidoc-gen`, `WORKDIR /workspace`, `ENTRYPOINT ["/usr/local/bin/apidoc-gen"]`.

---

## 5. CLI surface

Defined in `cmd/root.go`. The Cobra root has `Use: "apidoc-gen"` (note: not the actual binary name in either build path — see §11) and `Version: "1.0.0"` (hard-coded; bump here when releasing).

### Subcommands

| Command | Defined where | Purpose |
|---------|---------------|---------|
| `generate [path]` | `generateCmd` in `cmd/root.go` | Scan a Go project (path defaults to `.`) and emit docs. `Args: cobra.MaximumNArgs(1)`. |
| `init` | `initCmd` in `cmd/root.go` | Write `.apidoc-gen.yaml` to the current directory (no-op if it already exists). Pre-fills `framework:` from `go.mod` via `analyzer.DetectFramework`. |
| `completion <shell>` | Auto-injected by Cobra | bash / zsh / fish / powershell completions. Not declared in code. |
| `help [command]` | Auto-injected by Cobra | Standard help. |

### Persistent flags (root)

| Flag | Default | Notes |
|------|---------|-------|
| `--config` | `""` | Optional path to YAML config. If empty, `.apidoc-gen.yaml` is searched in `.`. |
| `-v, --verbose` | `false` | Verbose progress output. |
| `-q, --quiet` | `false` | Suppress progress; errors still go to stderr. |

### `generate` flags

| Flag | Default | Bound to viper key |
|------|---------|--------------------|
| `-o, --output` | `./docs` | `output` |
| `-t, --type` | `""` (must end up as `swagger`/`postman`/`custom`) | `type` |
| `-f, --framework` | `""` (auto-detect) | `framework` |
| `--interactive` | `true` | `interactive` |
| `-y, --no-interactive` | `false` | `no-interactive` |
| `--exclude` | `[]` (defaulted in `Validate`) | `exclude` |
| `--base-path` | `""` | `base-path` |
| `--title` | `"API Documentation"` | `title` |
| `--version` | `"1.0.0"` | `version` |
| `--description` | `""` | `description` |
| `--dry-run` | `false` | `dry-run` |
| `--show-config` | `false` | `show-config` |
| `--serve` | `false` | `serve` (only honored when `type == "swagger"`; serves `output` dir on `:8765`) |
| `--write-annotations` | `false` | `write-annotations` (writes swag `// @...` comment blocks above handler funcs in **same-file** handlers; cross-package handlers resolved via `resolveHandlerSourceFiles`) |
| `--upload` | `false` | `upload` (postman only; force upload, error if no key) |
| `--no-upload` | `false` | `no-upload` (postman only; suppress upload entirely) |
| `--postman-api-key` | `""` | `postman-api-key` (postman only; takes precedence over env / credentials file) |
| `--postman-workspace` | `""` | `postman-workspace` (postman only; UID; default = user's default workspace) |

### Configuration precedence

Lowest → highest: **config file → env → flags** (per `docs/CONFIGURATION.md` and `viper` setup in `initConfig`).

- Config file: `.apidoc-gen.yaml` in cwd, or `--config` path. Type is YAML (`viper.SetConfigType("yaml")`).
- Env prefix: `APIDOC_` (e.g. `APIDOC_TYPE=swagger`, `APIDOC_OUTPUT=./api-docs`). `viper.AutomaticEnv()` is on.
- Flags: as listed above.
- A `servers:` list in the config file is loaded via `viper.UnmarshalKey("servers", &cfg.Servers)`.
- **Secrets are *not* read from the config file.** The Postman API key is resolved separately from `--postman-api-key` → `APIDOC_POSTMAN_API_KEY` → `POSTMAN_API_KEY` → `~/.config/apidoc-gen/credentials.json` → interactive prompt. Don't add a code path that reads it from `.apidoc-gen.yaml`; that violates `SECURITY.md`.

### Exit codes

Defined in `cmd/root.go`:

```
ExitSuccess      = 0
ExitUsageError   = 1   // bad flags / invalid config / interactive failure
ExitRuntimeError = 2   // analyze or generate failed
```

Wrapped via the `exitCodeError` type so `runE` returns both an error and a code. Don't change these values without updating `README.md` ("Exit codes" line) and `docs/TROUBLESHOOTING.md`.

---

## 6. Internal architecture

### 6.1 Top-level run flow (`generate`)

`runGenerate` in `cmd/root.go`:

1. Determine `projectPath` from positional arg (default `.`).
2. Build `config.Config` from viper (then unmarshal `servers:` from the file).
3. Handle `--show-config` — print and exit.
4. If interactive and no `--type` set and not `--no-interactive`, run `prompt.GetUserPreferences`.
5. `cfg.Validate()` — checks path exists and `DocType ∈ {swagger,postman,custom}`; sets defaults.
6. Handle `--dry-run` — analyze only, print endpoint list, exit.
7. Build analyzer → `analyzer.NewAnalyzer(cfg).Analyze()` → `*models.APISpec`.
8. Build generator → `generator.NewGenerator(cfg.DocType, cfg).Generate(spec)`.
9. Print `printDocsAccessURL` (file URL + tip).
10. If `cfg.DocType == "postman"`, call `runPostmanUpload(cfg, interactive, quiet)` — see §6.8.
11. If `--write-annotations`, call `annotations.WriteSwagAnnotations(...)`.
12. If `--serve` and `type == swagger`, call `runServeDocs(cfg.Output, ...)` (blocks on `http.ListenAndServe`).

### 6.2 Analyzer (`pkg/analyzer/analyzer.go`)

Two-pass walk over `cfg.ProjectPath`:

- **Pass 1 — `collectTypesInFile`**: for every `.go` file (excluded dirs skipped via substring match on path), parses the file (no comments) and registers every `type X struct {...}` into `Analyzer.typeRegistry`. `goTypeToSchema` maps Go types to OpenAPI-style `models.Schema` (with `time.Time → string/date-time`, slices → arrays, maps → object with `additionalProperties`, named structs → `$ref: "#/components/schemas/<Name>"`).
- **Pass 2 — `parseFile`**: re-parses `.go` files with comments. For Gin/Echo/Fiber, first builds:
  - `curGroupPrefix` — variable name → joined path prefix from all chained `.Group("/x")` assignments.
  - `curAuthGroups` — variable name → true when `.Use(Auth())`, `.Use(JWT())`, or any middleware whose name contains `auth`/`jwt` (case-insensitive) is attached. These groups get `security: [{BearerAuth: []}]`.

  Then dispatches per framework:
  - **Chi**: `parseFile` exits the `ast.Inspect` loop early and instead walks every top-level `FuncDecl` body via `walkChiStmts(stmts, file, prefix, inheritedAuth)`. This recursive walker pre-scans each statement list for `Use(authMiddleware)` calls, then dispatches verb methods → `buildChiEndpoint`, `Route(path, fn)` → recursive call with accumulated prefix, `Group(fn)` → recursive call with the same prefix. This correctly handles arbitrary nesting of `r.Route` / `r.Group` closures that all reuse the identifier `r` (which would break a flat var-name map). Path parameters in `{id:[0-9]+}` brace form are normalized to `{id}` via `normalizeBracePath`.
  - `parseGinRoutes` (also used for Echo and Fiber) — recognizes `selector.METHOD("path", handler)` for METHOD ∈ {GET,POST,PUT,DELETE,PATCH,HEAD,OPTIONS}.
  - `parseGorillaMethods` + `parseGorillaRoutes` — handles `r.HandleFunc("/x", h).Methods("POST")`. A `consumedCalls map[*ast.CallExpr]bool` prevents duplicate endpoints when both the outer `.Methods()` node and inner `HandleFunc` node are visited by `ast.Inspect`. `buildGorillaSubrouterPrefixes` detects `child := parent.PathPrefix("/x").Subrouter()` chains.
  - `parseGenericRoutes` — last-resort generic `selector.METHOD("path", ...)` matcher when framework is `unknown`.

  For each endpoint, a shared `finishEndpoint(ep, handlerArg, file)` helper is called by all framework builders (Gin/Echo/Fiber, Gorilla, Chi). When the handler is a same-file ident:
  - `extractHandlerComments` reads the `funcDecl.Doc` block as `Description`, first line as `Summary` (if < 100 chars).
  - `getHandlerRequestAndResponseTypes` looks at the handler's 2nd parameter and 1st return value, then resolves them through `typeRegistry` to attach a 200 response and a `RequestBody` with the resolved schema. `addSchemaAndRefsToModels` recursively pulls referenced struct schemas into `spec.Models` so OpenAPI `components.schemas` resolves.
  - `extractQueryParams(file, funcName)` scans the handler body for `c.Query(name)`, `c.DefaultQuery(name, ...)`, `c.QueryParam(name)` (Gin/Echo/Fiber) and `r.URL.Query().Get(name)` (stdlib/Gorilla/Chi) call patterns, producing `Parameter{In: "query"}` entries. This runs for every endpoint regardless of framework.

  For cross-package handlers (e.g. `r.GET("/u", controllers.CreateUser)`), only `HandlerName` and `HandlerPackage` are recorded; same-file extraction is skipped. `resolveHandlerSourceFiles` later walks the project to find a file whose package or directory name matches `HandlerPackage` and that defines `HandlerName`, populating `SourceFile` (used by `--write-annotations`).

After both passes:
- `tagFromPath` adds a tag from the first non-`api`/non-`vN`/non-`:param`/non-`{param}` segment (used by Swagger UI grouping).
- `humanizeHandlerName("CreateProduct") → "Create product"` is used as a description fallback when no comment exists.
- `deduplicateEndpoints` keeps the first occurrence of each `(method, path)` pair.
- A default server `http://localhost:8080` is added if `cfg.Servers` is empty.

### 6.3 Generators (`pkg/generator/`)

`Generator` is a tiny interface:

```go
type Generator interface {
    Generate(spec *models.APISpec) error
}
```

`NewGenerator(docType, cfg)` switches on `docType` and returns one of three concrete generators. Each generator owns its own output-format conversion; **they do not share intermediate code beyond `models.APISpec`**.

- `SwaggerGenerator` (`swagger.go`):
  - Builds an OpenAPI 3.0.3 `map[string]interface{}` (not a typed struct), writes `openapi.yaml` (yaml.v3, indent 2), `openapi.json` (encoding/json, indent 2 spaces), and `index.html` (Swagger UI loaded from `cdn.jsdelivr.net`, fetches `./openapi.json`).
  - `BasePath` is prepended to every endpoint path during conversion.
  - Adds `components.securitySchemes.BearerAuth` (`type: http`, `scheme: bearer`, `bearerFormat: JWT`) the moment any endpoint declares `Security`.
  - `schemaToMap`: be aware that when a `Schema` has both a `Ref` and a default `type`, `out["type"] = "object"` is set first (line ~134), then `out["$ref"] = s.Ref` is added. Strict OpenAPI tooling expects `$ref` to have **no siblings** — if you touch this code, consider returning `{"$ref": s.Ref}` exclusively when `s.Ref != ""`.
- `PostmanGenerator` (`postman.go`):
  - Emits a single `collection.json` with a `baseUrl` collection variable (set to first server URL).
  - Groups endpoints into folders by tag if more than one tag exists.
  - Generates an example body with `generateExampleFromSchema` walking the struct.
  - `createPostmanURL` handles path-param variables: `models.Parameter.Example` is `interface{}` and is not set by the analyzer for path params. The type assertion is nil-guarded — when `Example` is nil the variable value defaults to `""`, otherwise `fmt.Sprint` is used. (Previously this was an unguarded `.(string)` that would panic; it is fixed. A regression test for this path does not yet exist — see §10.)
- `CustomGenerator` (`custom.go`):
  - Tries `npx create-docusaurus@latest <out> classic --skip-install`. If `npx` is missing or fails, falls back to `createMinimalDocusaurus` which writes a hand-rolled `package.json` (Docusaurus 3, React 18), `docusaurus.config.js`, `src/css/custom.css`, and the standard directory tree.
  - Generates a markdown page per endpoint (`<method>-<dashed-path>.md`) inside a directory per group (tag or first path segment, title-cased via `cases.Title(language.English)` from `golang.org/x/text/cases`).
  - Writes `sidebars.js` with an autogenerated category named "API Endpoints".
  - User then runs `npm install && npm start` in the output dir.

### 6.4 Models (`pkg/models/models.go`)

Single file, all types live here:

- `APISpec`, `Server`, `Endpoint`, `Parameter`, `RequestBody`, `Content`, `Response`, `Header`, `Schema`.
- `Endpoint` has internal-only fields (tagged `json:"-" yaml:"-"`): `SourceFile`, `HandlerName`, `HandlerPackage`, `RequestTypeName`, `ResponseTypeName`. These are used by `--write-annotations` and the analyzer's resolver, never serialized.
- `FrameWorkType` and `DocType` enumerate the framework/doc IDs as named string constants. **Note** `FrameWork` is intentionally CamelCased that way (treat it as a typo if you must, but rename consistently across all references; currently only `pkg/analyzer/analyzer.go` and the constant declarations use them).

### 6.5 Config (`pkg/config/config.go`)

`Config` is the runtime value used by every package. `Validate()`:

- Errors if `ProjectPath` does not exist.
- Errors if `DocType` is not one of `swagger`/`postman`/`custom`.
- **Mutates** the receiver to fill defaults for `Output` (`./docs`), `Title` (`"API Documentation"`), `Version` (`"1.0.0"`), and `Exclude`.
- Default `Exclude` set in code: `["vendor", "node_modules", "git", "test", "tests"]`. **Note** the entry is `"git"` (no dot), but `README.md` and `docs/CONFIGURATION.md` document `.git`. The analyzer uses `strings.Contains(path, exclude)` so both `"git"` and `".git"` happen to match, but documenting the discrepancy is honest. Don't "fix" silently — both behavior changes have user impact (e.g. `"git"` would also exclude a `git/` source dir).
- `ShouldExclude(path)` does an **exact equality** check, while the analyzer uses `strings.Contains`. The function exists but isn't used in the hot path; keep that in mind before "refactoring."

### 6.6 Interactive prompts (`internal/prompt/prompt.go`)

`GetUserPreferences(cfg)` walks the user through:

1. Doc type (`Swagger/OpenAPI`, `Postman Collection`, `Custom Docusaurus Site`).
2. Framework (`Auto-detect`, `Gin`, `Echo`, `Fiber`, `Gorilla Mux`, `Chi`).
3. API title, version, base path, output dir — each prompt seeded with the current `cfg` value.

The prompt is skipped entirely when `--no-interactive`/`-y` is set or when `--type` is provided non-empty.

### 6.7 Postman upload + desktop launch (`pkg/postman/` + `cmd/root.go::runPostmanUpload`)

Runs only when `cfg.DocType == "postman"`. Skipped entirely when `--no-upload` is set or when no `collection.json` was produced (defensive). Otherwise:

1. Resolve API key in this order: `--postman-api-key` flag → `APIDOC_POSTMAN_API_KEY` → `POSTMAN_API_KEY` → `~/.config/apidoc-gen/credentials.json` (`postman.LoadAPIKey`).
2. If still empty:
   - **Interactive mode** → prompt with `prompt.PromptPostmanAPIKey` (masked input, validates length only). Save with `postman.SaveAPIKey` (file mode `0600`, parent dir `0700`).
   - **`--no-interactive` + `--upload`** → return `&exitCodeError{..., ExitUsageError}` (exit 1).
   - **`--no-interactive` without `--upload`** → check `postman.IsDesktopInstalled()`:
     - Desktop installed → print `📦 Collection saved: <path>` + tip to run interactively or set the env key.
     - Desktop not installed → print `📦 Postman is not installed` + import instructions (download URL + web import URL).
     - Return `nil` in both cases. The local `collection.json` is always produced.
3. Read `<output>/collection.json` from disk.
4. `postman.LoadCachedUID(projectPath, title)` → if non-empty, `client.UpdateCollection(uid, body)` (PUT). On failure (e.g. 404 because the collection was deleted upstream), fall back to `CreateCollection`. Otherwise `CreateCollection(body, workspaceUID)` (POST).
5. `postman.SaveCachedUID(projectPath, title, uid)` writes the returned UID to `.apidoc-gen-cache.json`.
6. Print `https://go.postman.co/collection/<uid>` so the user clicks once and is in Postman.
7. **If `interactive && postman.IsDesktopInstalled()`** → call `postman.OpenDesktop(uid)` to launch the Postman desktop app and navigate directly to the collection via the `postman://app/collections/<uid>` URL scheme. Errors from `OpenDesktop` are non-fatal (printed to stderr as a warning); the collection is already on Postman cloud regardless. Desktop launch is intentionally skipped in non-interactive (`--no-interactive`) mode so CI pipelines are never affected.

#### Desktop detection and launch (`pkg/postman/desktop.go`)

`IsDesktopInstalled()` checks per OS:
- **macOS** — `/Applications/Postman.app` or `~/Applications/Postman.app`
- **Linux** — `postman` on PATH, or common install paths: `~/.local/share/Postman/Postman`, `/opt/Postman/Postman`, `/usr/bin/postman`, `/usr/local/bin/postman`
- **Windows** — presence of any entry under `%LOCALAPPDATA%\Postman\`

`OpenDesktop(uid)` launches via the `postman://` URL scheme:
- **macOS** — `open postman://app/collections/<uid>`
- **Linux** — `xdg-open postman://...` (falls back to launching the binary directly if `xdg-open` fails)
- **Windows** — `cmd /c start "" postman://...`

This is a local process launch, not a network call. No project data is sent.

#### Other implementation notes

- `pkg/postman/client.go` is stdlib-only (`net/http`, `encoding/json`). Auth header is `X-Api-Key`, `User-Agent: apidoc-gen`. 30s timeout per request. Errors include the upstream HTTP body (truncated to 200/400 chars) for debuggability.
- `UpdateCollection` calls `injectPostmanID` to set `info._postman_id = <uid>` before sending; without this the Postman API can reject the PUT.
- `wrapCollection` re-marshals `{"collection": <body>}`. The body must be a valid Postman v2.1 collection (which is what `pkg/generator/postman.go` writes — it includes `info.schema = ".../v2.1.0/..."`).
- The cache file `.apidoc-gen-cache.json` lives **in the project directory** (next to `.apidoc-gen.yaml`), not in the user's home. It contains no secrets — just `{"collections": {"<title>": "<uid>"}}`. It's `.gitignore`d.
- The credentials file `~/.config/apidoc-gen/credentials.json` is the **only** place the API key is persisted. It contains exactly `{"api_key": "..."}`. To revoke locally, run `rm ~/.config/apidoc-gen/credentials.json`.
- **Network calls**: uploading to `api.getpostman.com` is the only feature in the codebase that contacts a third-party server with project-derived data. `OpenDesktop` launches a local process and sends no data. If you add another network call (telemetry, AI enrichment, etc.), update `SECURITY.md` and this file.

### 6.8 Annotation writer (`internal/annotations/writer.go`)

`WriteSwagAnnotations(endpoints, basePath)`:

- Groups endpoints by `(SourceFile, HandlerName)`.
- For each group, parses the file, finds the function declaration line, builds a swag-style block (`// @Summary`, `// @Description`, `// @Tags`, `// @Accept json`, `// @Produce json`, `// @Param` per parameter and request body, `// @Success 200 {object} <Type>`, `// @Security BearerAuth` if applicable, `// @Router <path> [<method>]` for each path), then `insertOrReplaceSwagBlock` writes it directly above the function.
- **Replaces** any existing block of consecutive `// @...` comments above the function — so re-running is idempotent for blocks it generated, but it will overwrite hand-written swag annotations.
- This is a **side effect on user source files**. Treat any change to this code as security-sensitive.

---

## 7. Generated config-file schema (`.apidoc-gen.yaml`)

The exact format `init` writes (see `cmd/root.go` `runInit`):

```yaml
output: ./docs
type: swagger
framework: ""            # auto-filled with detected framework when init runs in a Go project
exclude:
  - vendor
  - node_modules
  - .git
  - test
  - tests
base_path: ""
title: "API Documentation"
version: "1.0.0"
description: "Auto-generated API documentation"
servers:
  - url: "http://localhost:8080"
    description: "Development server"
verbose: false
```

If you change available config keys, update **all** of:

1. `cmd/root.go` (flag declarations + `viper.BindPFlag` calls)
2. `pkg/config/config.go` (`Config` struct + `Validate`)
3. `cmd/root.go` `runInit` template
4. `docs/CONFIGURATION.md` table
5. `README.md` flags table

---

## 8. Documentation files

| File | Audience | Owns |
|------|----------|------|
| `README.md` | Users | Quick start, install, Docker, command/flag tables, project structure (high level), best practices |
| `docs/CONFIGURATION.md` | Users (deeper) | Flag/env/config-key precedence and defaults |
| `docs/TROUBLESHOOTING.md` | Users | PATH / no-endpoints / config-not-read / `npx` / CI / verbose |
| `docs/RICH_DOCS_DESIGN.md` | Maintainers | Phase 1 (static analysis) vs Phase 2 (optional AI) roadmap. Phase 1 marked Done. |
| `CONTRIBUTING.md` | Contributors | Setup, project layout, PR workflow |
| `CODE_OF_CONDUCT.md` | Contributors | Contributor Covenant 2.1 |
| `SECURITY.md` | Users / security | Local-only execution, no telemetry, what's read/written, vuln reporting |
| `LICENSE` | Everyone | MIT |
| `web/index.html` | Users | Standalone single-page getting-started UI (dark/light theme, no build step) |
| `AGENTS.md` | AI agents | This file. |

---

## 9. Conventions to follow when changing code

These conventions are derived from what the existing source already does. Don't fight them; reach out via an issue first if you want to break them (per `CONTRIBUTING.md`).

- **Style:** `gofmt`/`goimports`. New public APIs need a short doc comment (per `CONTRIBUTING.md`).
- **Cobra/viper:** When you add a new `generate` flag, also add the matching `viper.BindPFlag(...)` line right next to the others in `cmd/root.go`'s `init()`. Otherwise env-vars and config-file values won't override it.
- **Errors from `RunE`:** Wrap with `&exitCodeError{err: ..., code: ExitUsageError|ExitRuntimeError}` so the right exit code propagates. `Execute()` unwraps via `errors.As`.
- **Filesystem walks:** Both `analyzer.Analyze` and `findFileWithFunction` use `filepath.Walk` and skip directories whose path **contains** any string in `cfg.Exclude` via `strings.Contains`. If you switch to `filepath.WalkDir` or `os.DirFS`, preserve this substring-skip semantics (or document the change).
- **AST parsing:** Files that fail to parse are silently skipped (`return nil` from `parseFile` / `collectTypesInFile`). Don't make these fatal — Go projects often have generated code or build-tag-protected files that aren't parseable in isolation.
- **Output to stdout:** Progress messages go to stdout via `fmt.Println(...)`. Errors go to stderr via `fmt.Fprintln(os.Stderr, ...)`. `--quiet` only suppresses progress. Keep this distinction.
- **Emojis in CLI output:** The existing CLI uses `🔍 📊 📝 ✅ 📄 🌐 📦 💡 📖 🚀` in user-facing messages. Match the existing tone; don't add new ones gratuitously and don't strip the existing ones in normal edits — they're part of the UX (and `web/index.html` mirrors them).
- **`internal/` vs `pkg/`:** `pkg/` is consumable as a library by external code (e.g. someone could import `github.com/devenock/api-doc-gen/pkg/analyzer`); `internal/` is locked to this module. Place new framework-specific or output-specific functionality in `pkg/`; place CLI-only helpers and side-effecting utilities in `internal/`.

---

## 10. Tests

There are **no `_test.go` files anywhere in the tree** as of this scan. `make test` is currently a no-op (every package returns `?`). When you fix a bug, **add a test next to the file you changed** (`pkg/analyzer/analyzer_test.go`, etc.). Suggested seed cases that would have caught existing issues:

- A Postman-output integration test on a Gin app with `:id` path parameters (would catch the `param.Example.(string)` panic in `pkg/generator/postman.go`).
- A Swagger conversion test that confirms `$ref` schemas don't get sibling `type` keys (`pkg/generator/swagger.go` `schemaToMap`).
- A framework-detection test that verifies the substring matches in `DetectFramework` against fixture `go.mod` files.
- A `humanizeHandlerName` table test (cheap and isolated).

Don't claim coverage in commit messages without actually running `go test -cover ./...`.

---

## 11. Known inconsistencies and traps

These are **observations from the current scan**, not "TODOs from the maintainers." Surface them in PR descriptions if you touch the affected area; don't silently fix more than what your PR is scoped to.

1. **Three different binary names** in active use:
   - `Use:` field in Cobra: `apidoc-gen`
   - Built by `make build`: `bin/apidoc-gen`
   - Built by `go install <module>@latest`: **`api-doc-gen`** (with hyphen, derived from module path)
   - Documented in `README.md`/`docs/TROUBLESHOOTING.md`: **`api-doc-gen`** (the install path is canonical)
   - Legacy pre-built binary (now moved to `bin/api-doc-gen` and untracked): `api-doc-gen`
2. ~~**Three different declared Go versions**~~ **Fixed**: `go.mod`, `Dockerfile`, `README.md`, and `CONTRIBUTING.md` all agree on Go 1.24. If you bump the Go version again, update all four.
3. **Default exclude entry is `"git"` (no dot)** in `pkg/config/config.go`, but documentation says `.git`. Substring match means both work in practice, but the discrepancy is real.
4. **`pkg/generator/postman.go`** previously panicked on `param.Example.(string)` for path params (nothing sets `Example` in the analyzer). **Fixed** — the type assertion is nil-guarded (defaults to `""` if absent, otherwise `fmt.Sprint`). A regression test for this path does not yet exist (see §10). If you wire up real example values for path params, prefer setting them in the analyzer.
5. ~~**`schemaToMap`** emits both `type: object` and `$ref`~~ **Fixed**: `schemaToMap` in `pkg/generator/swagger.go` now returns `{"$ref": s.Ref}` exclusively when `s.Ref != ""`, so $ref schemas have no sibling keys.
6. **Echo/Fiber route parsing** delegates to `parseGinRoutes`. ~~Chi also delegated to parseGinRoutes~~ **Fixed**: Chi now has its own recursive `walkChiStmts` that handles nested `r.Route`/`r.Group` closures with correct prefix accumulation and auth inheritance (see §6.2). Fiber's `app.Static` is still not handled.
7. **Legacy pre-built binary**: a ~12 MB `api-doc-gen` (mach-o arm64) used to live at the repo root and was tracked in git. It has been moved to `./bin/api-doc-gen` and untracked via `git rm --cached`. It no longer appears in the repo layout (§2). The project rule going forward is that all build outputs live under `bin/` and stay out of git (see §4).
8. **`go.mod` lists every dependency as `// indirect`.** Likely an artifact of how it was last tidied. `go mod tidy` after any new direct import will refresh markers.
9. **`runInit`** silently returns success (and prints a friendly message) when `.apidoc-gen.yaml` already exists. It does not back up or merge. Don't change this without a flag like `--force`.
10. **`--serve`** is hard-coded to port `8765` and only works for `swagger` output. There's no `--port` flag.
11. **`completion`** subcommand is contributed by Cobra automatically; it isn't declared in `cmd/root.go`. If you want to disable or customize it, do so explicitly via `rootCmd.CompletionOptions`.
12. ~~**`pkg/generator/custom.go`** uses `strings.Title(...)`~~ **Fixed**: replaced with `cases.Title(language.English).String(...)` from `golang.org/x/text/cases`.
13. **Analyzer middleware-name detection** for auth uses the substring `auth` or `jwt` (case-insensitive) **plus** an explicit allow-list. Handlers wrapped via custom middleware names (e.g. `RequireRole("admin")`) won't be marked as protected.
14. **Postman upload is the only network call** the CLI makes with project-derived data. It is opt-in (no key → no call). `pkg/postman/desktop.go` (`OpenDesktop`) launches a local process via the `postman://` URL scheme — it does not transmit any data. The Swagger UI HTML references `cdn.jsdelivr.net` at *view time*, not at generation time. If you add another outbound network call (telemetry, AI enrichment, etc.), update `SECURITY.md` and §6.7.
15. **`SilenceUsage: true` and `SilenceErrors: true`** are set on every Cobra command. We print errors ourselves in `Execute()`; without these flags Cobra would print the error twice and follow it with the entire help/usage screen. Don't remove them without replacing that behavior.

---

## 12. When in doubt

- For **how to use the CLI as an end user**, defer to `README.md` and `docs/`.
- For **what the CLI is intended to grow into**, defer to `docs/RICH_DOCS_DESIGN.md`.
- For **PR/issue process**, defer to `CONTRIBUTING.md`.
- For **security/privacy claims** (no telemetry, local-only), defer to `SECURITY.md` and **don't introduce network calls or telemetry** without explicitly updating `SECURITY.md` first.

If a change you're about to make would invalidate any claim in this `AGENTS.md`, update this file in the same commit.
