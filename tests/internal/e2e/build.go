package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const buildTimeout = 2 * time.Minute

// BuildShaperBinary compiles the cmd/shaper entrypoint with the provided build tags and returns the binary path.
func BuildShaperBinary(tb testing.TB, repoRoot string, tags ...string) string {
	tb.Helper()

	if repoRoot == "" {
		tb.Fatal("repository root must be provided")
	}

	binaryPath := filepath.Join(tb.TempDir(), "shaper")

	args := []string{"build", "-o", binaryPath}
	if len(tags) > 0 {
		args = append(args, "-tags", strings.Join(tags, ","))
	}

	args = append(args, "./cmd/shaper")

	ctx, cancel := context.WithTimeout(context.Background(), buildTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = repoRoot

	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		tb.Fatalf("build shaper binary: %v\n%s", err, output)
	}

	return binaryPath
}
