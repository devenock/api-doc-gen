# Troubleshooting

## No endpoints found

- **Framework detection** – Ensure your framework is listed in `go.mod` (Gin, Echo, Fiber, Gorilla, Chi). Or set `--framework` explicitly.
- **Route patterns** – The analyzer looks for standard patterns (e.g. `router.GET("/path", handler)`). Use `-v` (verbose) to see the detected framework.
- **Excluded dirs** – Check that your route files are not under an excluded directory (e.g. `vendor`, `test`). Adjust `exclude` in config or `--exclude` if needed.
- **Path** – Run from the project root or pass the project path: `apidoc-gen generate /path/to/project`.

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
