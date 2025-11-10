package buildinfo_test

//nolint:depguard // buildinfo tests import the package under test
import (
	"testing"

	"oci-cpu-shaper/internal/buildinfo"
)

func TestCurrentReturnsInjectedMetadata(t *testing.T) {
	t.Parallel()

	originalVersion, originalCommit, originalDate := buildinfo.Version, buildinfo.GitCommit, buildinfo.BuildDate
	buildinfo.Version = "1.2.3-test"
	buildinfo.GitCommit = "abcdef123456"
	buildinfo.BuildDate = "2024-05-01T00:00:00Z"

	t.Cleanup(func() {
		buildinfo.Version = originalVersion
		buildinfo.GitCommit = originalCommit
		buildinfo.BuildDate = originalDate
	})

	info := buildinfo.Current()
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
