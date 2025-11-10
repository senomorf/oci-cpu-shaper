package buildinfo

import "testing"

func TestCurrentReturnsInjectedMetadata(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, GitCommit, BuildDate
	Version = "1.2.3-test"
	GitCommit = "abcdef123456"
	BuildDate = "2024-05-01T00:00:00Z"
	t.Cleanup(func() {
		Version = originalVersion
		GitCommit = originalCommit
		BuildDate = originalDate
	})

	info := Current()
	if info.Version != "1.2.3-test" {
		t.Fatalf("expected version \"1.2.3-test\", got %q", info.Version)
	}
	if info.GitCommit != "abcdef123456" {
		t.Fatalf("expected git commit \"abcdef123456\", got %q", info.GitCommit)
	}
	if info.BuildDate != "2024-05-01T00:00:00Z" {
		t.Fatalf("expected build date \"2024-05-01T00:00:00Z\", got %q", info.BuildDate)
	}
}
