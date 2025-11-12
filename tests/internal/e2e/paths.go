package e2e

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// RepositoryRoot returns the repository root by walking two directories up from the tests package.
func RepositoryRoot(tb testing.TB) string {
	tb.Helper()

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		tb.Fatal("determine caller path")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(currentFile), "..", "..", ".."))
	modPath := filepath.Join(root, "go.mod")

	_, err := os.Stat(modPath)
	if err != nil {
		tb.Fatalf("locate repository root: %v", err)
	}

	return root
}
