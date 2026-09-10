package analyzer

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"time"
)

// BuildCheckResult is the outcome of CheckBuild.
type BuildCheckResult struct {
	Skipped bool

	OK bool

	Output string

	Err error
}

const buildCheckTimeout = 2 * time.Minute

func CheckBuild(projectPath string) BuildCheckResult {
	goPath, err := exec.LookPath("go")
	if err != nil {
		return BuildCheckResult{Skipped: true}
	}

	ctx, cancel := context.WithTimeout(context.Background(), buildCheckTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, goPath, "vet", "./...")
	cmd.Dir = projectPath

	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
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
