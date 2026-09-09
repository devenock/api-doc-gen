# Configuration Reference

Configuration is merged from lowest to highest precedence: **config file** → **env vars** → **flags**. Flags always win.

## Precedence

1. **Config file** — `.specyl.yaml` in the current directory (or the path set by `--config`)
2. **Env vars** — `SPECYL_<KEY>` (e.g. `SPECYL_TYPE`, `SPECYL_OUTPUT`)
3. **Flags** — e.g. `--type swagger`, `-o ./docs`

## Flag · env · config key mapping

| Flag | Env var | Config key | Default |
|------|---------|------------|---------|
| `--output`, `-o` | `SPECYL_OUTPUT` | `output` | `./docs` |
| `--type`, `-t` | `SPECYL_TYPE` | `type` | _(none)_ |
| `--framework`, `-f` | `SPECYL_FRAMEWORK` | `framework` | _(auto-detect)_ |
| `--base-path` | `SPECYL_BASE_PATH` | `base_path` | `""` |
| `--title` | `SPECYL_TITLE` | `title` | auto-detected from `go.mod`'s module name (falls back to `API Documentation` if `go.mod` can't be read) |
| `--version` | `SPECYL_VERSION` | `version` | `1.0.0` |
| `--description` | `SPECYL_DESCRIPTION` | `description` | `""` |
| `--exclude` | `SPECYL_EXCLUDE` | `exclude` | see below |
| `--no-interactive`, `-y` | _(n/a)_ | _(n/a)_ | `false` |
| `--verbose`, `-v` | `SPECYL_VERBOSE` | `verbose` | `false` |
| `--quiet`, `-q` | `SPECYL_QUIET` | `quiet` | `false` |
| `--dry-run` | _(n/a)_ | _(n/a)_ | `false` |
| `--show-config` | _(n/a)_ | _(n/a)_ | `false` |
| `--serve` _(swagger)_ | _(n/a)_ | _(n/a)_ | `true` — after generating, serves `./docs` and opens it in your browser (on the app's own detected port when found in its source, falling back to `8765` if that port is taken or nothing was detected); pass `--serve=false` to skip |
| `--write-annotations` | _(n/a)_ | _(n/a)_ | `false` — writes swag-style `// @...` comments above same-file handler functions |
| `--skip-build-check` | _(n/a)_ | _(n/a)_ | `false` — skips the `go vet ./...` pre-flight check against the target project |
| `--required-by-default` | _(n/a)_ | _(n/a)_ | `false` — marks every struct field required unless it has `json:",omitempty"`, instead of only fields with an explicit `binding`/`validate:"required"` tag |
| `--tags` | _(n/a)_ | _(n/a)_ | _(none — keeps everything)_ — filter endpoints by tag; a plain name includes only endpoints with that tag, a `!name` excludes them. Mixable, comma-separated |
| `[path]` _(positional)_ | _(n/a)_ | _(n/a)_ | `.` |

Default `exclude` dirs: `vendor`, `node_modules`, `.git`, `test`, `tests`. Matching is by exact directory name (basename), not substring.

Passing `--exclude` (or setting `exclude` in the config file) **replaces** this default list rather than adding to it — if you still want the defaults skipped, list them alongside your own, e.g. `--exclude vendor,node_modules,.git,test,tests,fixtures`.

## Auth middleware detection

Config-file only (no flag/env equivalent). By default, a route group is marked authenticated (`security: [BearerAuth]` in the generated spec) when its middleware's name contains `auth` or `jwt` (case-insensitive), plus a small set of common exact names. Set `auth_middleware` in `.specyl.yaml` to override that heuristic entirely with an exact (case-insensitive) list of your own middleware names:

```yaml
auth_middleware:
  - requireSession
  - JWTAuth
```

Run with `-v` to see exactly what was matched, either way.

## Inspect effective config

```bash
specyl generate --show-config
```

Prints the merged values and exits without generating or prompting.
