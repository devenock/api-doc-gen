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
| `[path]` (positional)  | (n/a)             | (n/a)            | `.`              |
| (servers)              | (n/a)             | `servers`        | (default server) |

Default `exclude` when not set: `vendor`, `node_modules`, `git`, `test`, `tests`.

## Viewing effective config

To see the merged configuration (after file, env, and flags):

```bash
apidoc-gen generate --show-config
```

This prints the effective values and exits without generating or prompting.
