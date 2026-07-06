# Troubleshooting

## Command not found after `go install`

The binary is named **`api-doc-gen`** (with a hyphen). If the shell says it cannot find it, Go's bin directory is not on your PATH.

**Fix:**

```bash
# Find where Go installs binaries
go env GOPATH   # empty means $HOME/go

# Add to ~/.zshrc or ~/.bashrc
export PATH="$PATH:$(go env GOPATH)/bin"

# Reload
source ~/.zshrc
```

Verify with `api-doc-gen --version`. Or run by full path:

```bash
$(go env GOPATH)/bin/api-doc-gen generate --no-interactive --type swagger -o ./docs
```

## No endpoints found

- **Framework not detected** — ensure your framework is in `go.mod`. Set `--framework` explicitly if auto-detection misses it: `--framework gin`.
- **Wrong directory** — run from the project root (where `go.mod` lives), or pass the path: `api-doc-gen generate /path/to/project`.
- **Excluded directories** — route files inside `vendor`, `test`, or `tests` are skipped by default. Override with `--exclude ""` or adjust `exclude` in `.apidoc-gen.yaml`.
- **Verbose output** — run with `-v` to see the detected framework and how many files were scanned.

## Invalid configuration

- **"project path does not exist"** — the path you passed (or `.`) is not a directory. Run from the correct root or pass a valid path.
- **"invalid documentation type"** — `--type` must be `swagger` or `postman`. Run in interactive mode or pass `--type` explicitly.

## Config file not being read

- Ensure `.apidoc-gen.yaml` is in the **current working directory** when you run the command, not a parent or child directory.
- Check for YAML syntax errors (no tabs, correct indentation).
- Env vars must be prefixed with `APIDOC_` (e.g. `APIDOC_TYPE=swagger`).
- Inspect what is actually loaded: `api-doc-gen generate --show-config`

## Prompts appearing in CI

The CLI is interactive by default. To disable all prompts:

```bash
api-doc-gen generate --no-interactive --type swagger -o ./docs
```

Both `--no-interactive` (or `-y`) and `--type` are required — without `--type` the CLI still needs to ask which format to generate.

**Exit codes:** `0` success · `1` validation/usage error · `2` runtime error.

## Quiet and verbose modes

- `-q` / `--quiet` — suppress all progress output. Errors still go to stderr.
- `-v` / `--verbose` — show detected framework, file count, endpoint list.
