package analyzer

import (
	"bytes"
	"context"
	"os/exec"
	"time"
)

// BuildCheckResult is the outcome of CheckBuild.
type BuildCheckResult struct {
	// Skipped is true when the check could not be attempted at all (no Go
	// toolchain on PATH) — distinct from OK=false, which means the
	// toolchain ran and found a problem. The tool's own Docker runtime
	// image ships without a Go toolchain by design (a slim alpine image
	// with just the compiled binary), so this is an expected, common case
	// there, not a failure.
	Skipped bool
	// OK is true when `go vet ./...` succeeded against the target project.
	// Only meaningful when Skipped is false.
	OK bool
	// Output is go vet's combined output, populated when OK is false.
	Output string
	// Err is set when the check errored for a reason other than vet
	// reporting problems (e.g. it timed out).
	Err error
}

// buildCheckTimeout bounds how long CheckBuild will wait for `go vet` — long
// enough for a first-run dependency download on a reasonably sized project,
// short enough not to hang a `generate` invocation indefinitely.
const buildCheckTimeout = 2 * time.Minute

// CheckBuild runs `go vet ./...` against the project at projectPath as a
// pre-flight safety check: does this project actually compile (and pass
// vet's other correctness checks) before api-doc-gen spends time analyzing
// it? This intentionally does not change how routes/types are resolved —
// the analyzer stays AST-only — it only tells the caller up front whether
// the project is in a state where that analysis can be trusted to be
// complete, so an incomplete result (from a project with build errors)
// isn't mistaken for "these really are all the routes."
//
// go vet is used rather than go build: it never writes build artifacts
// (nothing to clean up, nothing risks landing in the target project's
// directory), and it requires the same successful compilation as build
// while also catching a broader class of real correctness issues, which
// fits a tool meant to flag "is this project safe to trust" rather than
// narrowly "does `go build` succeed."
func CheckBuild(projectPath string) BuildCheckResult {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return BuildCheckResult{Skipped: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), buildCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, goPath, "vet", "./...")
	cmd.Dir = projectPath
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	runErr := cmd.Run()

	if ctx.Err() == context.DeadlineExceeded {
		return BuildCheckResult{Err: context.DeadlineExceeded}
	}
	if runErr != nil {
		return BuildCheckResult{OK: false, Output: out.String()}
	}
	return BuildCheckResult{OK: true}
}
