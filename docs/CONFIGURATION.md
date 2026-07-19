# Configuration Reference

Configuration is merged from lowest to highest precedence: **config file** → **env vars** → **flags**. Flags always win.

## Precedence

1. **Config file** — `.apidoc-gen.yaml` in the current directory (or the path set by `--config`)
2. **Env vars** — `APIDOC_<KEY>` (e.g. `APIDOC_TYPE`, `APIDOC_OUTPUT`)
3. **Flags** — e.g. `--type swagger`, `-o ./docs`

## Flag · env · config key mapping

| Flag | Env var | Config key | Default |
|------|---------|------------|---------|
| `--output`, `-o` | `APIDOC_OUTPUT` | `output` | `./docs` |
| `--type`, `-t` | `APIDOC_TYPE` | `type` | _(none)_ |
| `--framework`, `-f` | `APIDOC_FRAMEWORK` | `framework` | _(auto-detect)_ |
| `--base-path` | `APIDOC_BASE_PATH` | `base_path` | `""` |
| `--title` | `APIDOC_TITLE` | `title` | auto-detected from `go.mod`'s module name (falls back to `API Documentation` if `go.mod` can't be read) |
| `--version` | `APIDOC_VERSION` | `version` | `1.0.0` |
| `--description` | `APIDOC_DESCRIPTION` | `description` | `""` |
| `--exclude` | `APIDOC_EXCLUDE` | `exclude` | see below |
| `--no-interactive`, `-y` | _(n/a)_ | _(n/a)_ | `false` |
| `--verbose`, `-v` | `APIDOC_VERBOSE` | `verbose` | `false` |
| `--quiet`, `-q` | `APIDOC_QUIET` | `quiet` | `false` |
| `--dry-run` | _(n/a)_ | _(n/a)_ | `false` |
| `--show-config` | _(n/a)_ | _(n/a)_ | `false` |
| `--serve` _(swagger)_ | _(n/a)_ | _(n/a)_ | `false` — after generating, serves `./docs` at `http://localhost:8765` and opens it in your browser |
| `--write-annotations` | _(n/a)_ | _(n/a)_ | `false` — writes swag-style `// @...` comments above same-file handler functions |
| `--upload` _(postman)_ | _(n/a)_ | _(n/a)_ | `false` |
| `--no-upload` _(postman)_ | _(n/a)_ | _(n/a)_ | `false` |
| `--direct-import` _(postman)_ | _(n/a)_ | _(n/a)_ | `false` — no functional effect outside the interactive wizard today (see note below) |
| `--postman-api-key` | `APIDOC_POSTMAN_API_KEY` · `POSTMAN_API_KEY` | _(not in config — secret)_ | _(none)_ |
| `--postman-workspace` | _(n/a)_ | _(n/a)_ | _(default workspace)_ |
| `[path]` _(positional)_ | _(n/a)_ | _(n/a)_ | `.` |

Default `exclude` dirs: `vendor`, `node_modules`, `.git`, `test`, `tests`. Matching is by exact directory name (basename), not substring.

## Postman API key resolution

The Postman API key is never stored in `.apidoc-gen.yaml`. Resolution order:

1. `--postman-api-key <key>`
2. `APIDOC_POSTMAN_API_KEY`
3. `POSTMAN_API_KEY`
4. `~/.config/apidoc-gen/credentials.json` (written on first interactive prompt; mode `0600`)
5. Interactive prompt (only when `--no-interactive` is not set)

## Upload behaviour matrix

| Mode | Key available? | `--upload` | `--no-upload` | Result |
|------|---------------|------------|---------------|--------|
| Interactive | yes | — | no | Auto-upload, print Postman URL |
| Interactive | no | no | no | Prompt for key, save, upload |
| Interactive | — | — | yes | Skip upload |
| `--no-interactive` | yes | — | no | Auto-upload |
| `--no-interactive` | no | no | no | Skip silently, print local-file path |
| `--no-interactive` | no | yes | no | **Error** (exit code 1) |
| any | — | — | yes | Skip upload |

The collection UID is cached in `.apidoc-gen-cache.json` (project root, gitignored by default). Delete it to force a new collection on the next upload.

**`--direct-import` / the wizard's "Import directly into Postman" option** currently produce the same outcome as doing nothing: if Postman desktop is installed, it opens; either way you still drag `collection.json` into the sidebar (or File > Import) yourself. There is no automatic local-file import yet — only `--upload` (cloud API) results in Postman opening pre-loaded with the collection.

## Inspect effective config

```bash
api-doc-gen generate --show-config
```

Prints the merged values and exits without generating or prompting.
