# Troubleshooting

## Command not found after `go install`

If `go install github.com/devenock/api-doc-gen@latest` succeeds but running the tool gives **command not found**, check:

1. **Command name** – The binary is named **`api-doc-gen`** (with a hyphen), from the module path. Use `api-doc-gen init` and `api-doc-gen generate`, not `apidoc-gen`.
2. **PATH** – Your shell cannot see the binary because Go’s bin directory is not in your PATH.

**Fix:** Add Go’s bin directory to your PATH.

1. **Find where the binary was installed:**
   ```bash
   go env GOPATH
   ```
   If empty, Go uses `$HOME/go`. The binary is in `$GOPATH/bin` or `$HOME/go/bin`.

2. **Add it to PATH** (pick one that matches your shell).

   **Bash** – add to `~/.bashrc` (or `~/.bash_profile`):
   ```bash
   export PATH="$PATH:$(go env GOPATH)/bin"
   ```

   **Zsh** – add to `~/.zshrc`:
   ```bash
   export PATH="$PATH:$(go env GOPATH)/bin"
   ```

   **Fish** – run once or add to config:
   ```fish
   set -gx PATH $PATH (go env GOPATH)/bin
   ```

3. **Reload your shell** (e.g. open a new terminal or run `source ~/.zshrc`).

4. **Verify:** `api-doc-gen --version`

**Alternative:** Run by full path once you know it:
```bash
$(go env GOPATH)/bin/api-doc-gen init
$(go env GOPATH)/bin/api-doc-gen generate --no-interactive --type swagger -o ./docs
```

## Panic: "unsupported flag … omitempty" when generating Swagger

You are running an older binary that had a YAML encoding bug. **Install the latest code from this repo** so the fixed CLI is in your PATH:

```bash
cd /path/to/api-doc-gen   # this repository
go install .
```

Then from your backend project run `api-doc-gen generate --no-interactive --type swagger -o ./docs` again. The new binary is used globally (same `api-doc-gen` command from any directory).

## No endpoints found

- **Framework detection** – Ensure your framework is listed in `go.mod` (Gin, Echo, Fiber, Gorilla, Chi). Or set `--framework` explicitly.
- **Route patterns** – The analyzer looks for standard patterns (e.g. `router.GET("/path", handler)`). Use `-v` (verbose) to see the detected framework.
- **Excluded dirs** – Check that your route files are not under an excluded directory (e.g. `vendor`, `test`). Adjust `exclude` in config or `--exclude` if needed.
- **Path** – Run from the project root or pass the project path: `api-doc-gen generate /path/to/project`.

## Invalid configuration

- **"project path does not exist"** – The path you passed (or `.`) is not a directory. Run from the project root or pass a valid path.
- **"invalid documentation type"** – You must use `swagger`, `postman`, or `custom`. Set `--type` or run in interactive mode to choose.

## Configuration not being read

- Ensure `.apidoc-gen.yaml` is in the current directory, or pass `--config /path/to/config.yaml`.
- Check YAML syntax (indentation, no tabs). Use `apidoc-gen generate --show-config` to see what is actually loaded.
- Environment variables must be prefixed with `APIDOC_` (e.g. `APIDOC_TYPE=swagger`).

## npx not found (Custom Docusaurus)

- Install [Node.js and npm](https://nodejs.org) and ensure `npx` is on your PATH.
- The custom generator runs `npx create-docusaurus@latest`; without Node/npm it will fall back to a minimal structure.

## Scripts / CI: prompts or wrong behavior

- Use **non-interactive** mode so the CLI does not wait for input:
  - Set `--type` (and other required options), or
  - Use `--no-interactive` (or `-y`) and ensure all required flags are set.
- Rely on **exit codes**: `0` = success, `1` = usage/validation error, `2` = runtime error (e.g. analysis or generation failed).

## Quiet / verbose

- Use `-q` / `--quiet` to suppress progress messages (e.g. in CI logs). Errors are still printed to stderr.
- Use `-v` / `--verbose` for more detail (detected framework, endpoint count).
