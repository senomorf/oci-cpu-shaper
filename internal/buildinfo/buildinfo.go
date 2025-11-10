// Package buildinfo exposes version metadata injected at build time.
package buildinfo

// Info captures identifying metadata for a build of the CPU shaper.
type Info struct {
	Version   string
	GitCommit string
	BuildDate string
}

// These variables are intended to be overridden via -ldflags during release builds.
var (
	Version   = "dev"
	GitCommit = "unknown"
	BuildDate = "unknown"
)

// Current returns the build metadata for logging and diagnostics.
func Current() Info {
	return Info{
		Version:   Version,
		GitCommit: GitCommit,
		BuildDate: BuildDate,
	}
}
