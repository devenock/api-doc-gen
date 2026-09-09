# Security Policy

## Supported versions

`specyl` does not yet have tagged releases with a formal support window. Security fixes are made against the `main` branch; please run the latest version from `main` or `go install github.com/devenock/specyl@latest`.

## Reporting a vulnerability

Please report security issues privately using [GitHub's private vulnerability reporting](https://github.com/devenock/specyl/security/advisories/new) for this repository, rather than opening a public issue. This lets us investigate and prepare a fix before the details are public.

Include, where possible:

- A description of the issue and its potential impact
- Steps to reproduce (a minimal target project that triggers it is ideal, since this tool's main attack surface is *analyzing* a target codebase)
- The version/commit you tested against

We'll acknowledge reports as promptly as we can and keep you updated as a fix is prepared.

## Scope

`specyl` parses and analyzes a target Go project's source (AST-only — it never executes the target project's code) to generate API documentation. Security-relevant areas include:

- Path traversal or symlink-following while walking a target project (see `pkg/analyzer`'s symlink-safety checks)
- Injection into generated output that gets rendered or executed elsewhere (e.g. the generated Swagger UI HTML)
- The optional `go vet ./...` pre-flight build check, which does invoke the Go toolchain against the target project's code

Issues in a *target project being analyzed* (i.e., bugs in someone else's Go code) are out of scope unless specyl's handling of that code creates a vulnerability in specyl itself or in its output.
