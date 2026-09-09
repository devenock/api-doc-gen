# Troubleshooting

## Command not found after `go install`

The binary is named **`specyl`** (with a hyphen). If the shell says it cannot find it, Go's bin directory is not on your PATH.

**Fix:**

```bash
# Find where Go installs binaries
go env GOPATH   # empty means $HOME/go

# Add to ~/.zshrc or ~/.bashrc
export PATH="$PATH:$(go env GOPATH)/bin"

# Reload
source ~/.zshrc
```

Verify with `specyl --version`. Or run by full path:

```bash
$(go env GOPATH)/bin/specyl generate --no-interactive --type swagger -o ./docs
```

## No endpoints found

If the analyzer finds zero endpoints, the CLI prints a warning by default (no `-v` needed) showing the detected framework and the likely cause, right before it writes the (empty) output file. If you see that warning:

- **Framework not detected** — ensure your framework is in `go.mod`. Set `--framework` explicitly if auto-detection misses it: `--framework gin`. Supported: `gin`, `echo`, `fiber`, `gorilla`, `chi`.
- **Wrong directory** — run from the project root (where `go.mod` lives), or pass the path: `specyl generate /path/to/project`.
- **Excluded directories** — route files inside `vendor`, `test`, or `tests` are skipped by default. Override with `--exclude ""` or adjust `exclude` in `.specyl.yaml`.
- **Verbose output** — run with `-v` to also see the file count and the full endpoint list once found, or `--dry-run` to inspect without writing files.

## "Multiple frameworks detected in go.mod"

`go.mod` has more than one supported framework as a direct dependency (e.g. Gin for the API, Chi vendored in a subpackage) — auto-detection deliberately refuses to guess between them, since silently picking the wrong one is worse than an honest "unknown." Pass `--framework` explicitly to disambiguate: `--framework gin`. A framework only listed as `// indirect` in `go.mod` (pulled in transitively by another dependency, never imported by your own code) doesn't count and won't trigger this.

## "This project does not currently pass `go vet ./...`"

Before analyzing, `generate` runs `go vet ./...` against the target project as an advisory pre-flight check — a project with build errors can still be partially analyzed (the AST parser doesn't require type-correct code), but the result may be incomplete or misleading. This warning never blocks generation.

- Run with `-v` to see the full `go vet` output.
- Pass `--skip-build-check` to disable the check entirely (e.g. running against a branch mid-refactor in CI).
- The check no-ops automatically wherever the `go` toolchain isn't on `PATH` — including this project's own Docker runtime image, a slim `alpine` base with just the compiled binary.

## "N file(s) could not be parsed"

One or more `.go` files matched by the project walk have a syntax error (or other parse failure) and were skipped — every route or type that file would have contributed is missing from the output, with no other indication. Run with `-v` to see which files and why, then fix the syntax error (or add the directory to `exclude` if it's intentionally invalid, e.g. a template or fixture directory).

## Invalid configuration

- **"project path does not exist"** — the path you passed (or `.`) is not a directory. Run from the correct root or pass a valid path.
- **"invalid documentation type"** — `--type` must be `swagger` or `postman`. Run in interactive mode or pass `--type` explicitly.

## Config file not being read

- Ensure `.specyl.yaml` is in the **current working directory** when you run the command, not a parent or child directory.
- Check for YAML syntax errors (no tabs, correct indentation).
- Env vars must be prefixed with `SPECYL_` (e.g. `SPECYL_TYPE=swagger`).
- Inspect what is actually loaded: `specyl generate --show-config`

## Prompts appearing in CI

The CLI is interactive by default. To disable all prompts:

```bash
specyl generate --no-interactive --type swagger -o ./docs
```

Both `--no-interactive` (or `-y`) and `--type` are required — without `--type` the CLI still needs to ask which format to generate.

**Exit codes:** `0` success · `1` validation/usage error · `2` runtime error.

## Quiet and verbose modes

- `-q` / `--quiet` — suppress all progress output, including the advisory warnings above (build check, parse failures, multiple frameworks). Hard errors still go to stderr.
- `-v` / `--verbose` — shows detected framework, file count, and the full endpoint list, plus:
  - full `go vet` output when the build check fails
  - which files failed to parse and why
  - which middleware names were matched as auth (built-in heuristic or your configured `auth_middleware` list — see [CONFIGURATION.md](CONFIGURATION.md#auth-middleware-detection))
  - which call names were matched as request-body binding by name hint (e.g. a project-specific `decodeBody` wrapper), rather than an exact known framework method
