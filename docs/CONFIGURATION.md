# Configuration Reference

Configuration is merged from (lowest to highest precedence): **config file** → **environment variables** → **flags**. Later sources override earlier ones.

## Precedence

1. **Config file** – `.apidoc-gen.yaml` in the current directory (or path set by `--config`)
2. **Environment variables** – `APIDOC_<KEY>` (e.g. `APIDOC_TYPE`, `APIDOC_OUTPUT`)
3. **Command-line flags** – e.g. `--type swagger`, `-o ./docs`

## Flag, env, and config key mapping

| Flag / behavior        | Env variable      | Config file key | Default          |
|------------------------|-------------------|------------------|------------------|
| `--output`, `-o`       | `APIDOC_OUTPUT`   | `output`         | `./docs`         |
| `--type`, `-t`         | `APIDOC_TYPE`     | `type`           | (none)           |
| `--framework`, `-f`    | `APIDOC_FRAMEWORK`| `framework`     | (auto-detect)    |
| `--base-path`          | `APIDOC_BASE_PATH`| `base_path`     | `""`             |
| `--title`              | `APIDOC_TITLE`    | `title`          | `API Documentation` |
| `--version`            | `APIDOC_VERSION`  | `version`        | `1.0.0`          |
| `--description`        | `APIDOC_DESCRIPTION` | `description` | `""`          |
| `--exclude`             | `APIDOC_EXCLUDE`  | `exclude`        | (see below)      |
| `--interactive`        | `APIDOC_INTERACTIVE` | `interactive` | `true`        |
| `--no-interactive`, `-y` | (n/a)            | (n/a)            | `false`         |
| `--verbose`, `-v`      | `APIDOC_VERBOSE`  | `verbose`        | `false`         |
| `--quiet`, `-q`        | `APIDOC_QUIET`    | `quiet`          | `false`         |
| `--config`             | (n/a)             | (n/a)            | `.apidoc-gen.yaml` |
| `--dry-run`            | (n/a)             | (n/a)            | `false`         |
| `--show-config`        | (n/a)             | (n/a)            | `false`         |
| `--serve`              | (n/a)             | (n/a)            | `false`         |
| `--write-annotations`  | (n/a)             | (n/a)            | `false`         |
| `--upload` (postman)   | (n/a)             | (n/a)            | `false`         |
| `--no-upload` (postman)| (n/a)             | (n/a)            | `false`         |
| `--postman-api-key`    | `APIDOC_POSTMAN_API_KEY` (also `POSTMAN_API_KEY`) | (not in config file — secret) | (none) |
| `--postman-workspace`  | (n/a)             | (n/a)            | (default workspace) |
| `[path]` (positional)  | (n/a)             | (n/a)            | `.`              |
| (servers)              | (n/a)             | `servers`        | (default server) |

Default `exclude` when not set: `vendor`, `node_modules`, `git`, `test`, `tests`.

## Postman upload (only when `type: postman`)

The Postman API key is **never read from `.apidoc-gen.yaml`** (see `SECURITY.md`). Resolution order:

1. `--postman-api-key <key>`
2. `APIDOC_POSTMAN_API_KEY`
3. `POSTMAN_API_KEY`
4. `~/.config/apidoc-gen/credentials.json` (written by the interactive prompt; mode `0600`)
5. Interactive prompt (only when **not** `--no-interactive`)

Behavior matrix:

| Mode | Key available? | `--upload` | `--no-upload` | What happens |
|------|----------------|------------|---------------|--------------|
| Interactive | yes | — | no | Auto-upload, print Postman URL |
| Interactive | no | no | no | Prompt for key, save it, upload |
| Interactive | — | — | yes | Skip upload |
| `--no-interactive` | yes | — | no | Auto-upload |
| `--no-interactive` | no | no | no | Skip upload silently, print local-file tip |
| `--no-interactive` | no | yes | no | **Error**, exit code `1` |
| any | — | — | yes | Skip upload |

Cache file `.apidoc-gen-cache.json` (in the project root, `.gitignore`d by default): `{ "collections": { "<collection title>": "<postman uid>" } }`. Delete to force "create new" behavior on next upload.

## Viewing effective config

To see the merged configuration (after file, env, and flags):

```bash
apidoc-gen generate --show-config
```

This prints the effective values and exits without generating or prompting.
