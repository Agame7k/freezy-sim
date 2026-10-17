// Package version holds the simulator version, stamped into release builds.
package version

import (
	"runtime"
	"runtime/debug"
)

// Version is the simulator version string. Release builds set it (release.cmd):
//
//	go build -ldflags "-X github.com/Agame7k/freezy-sim/internal/version.Version=0.1.0" ./cmd/freezy-sim
var Version = "0.1.0-dev"

// Describe is the version plus the commit and Go version the binary was built from, for -version, logs and reports.
func Describe() string {
	text := "Freezy Sim " + Version
	if info, ok := debug.ReadBuildInfo(); ok {
		var revision, modified string
		for _, setting := range info.Settings {
			switch setting.Key {
			case "vcs.revision":
				revision = setting.Value
			case "vcs.modified":
				if setting.Value == "true" {
					modified = "+changes"
				}
			}
		}
		if len(revision) > 7 {
			text += " (commit " + revision[:7] + modified + ")"
		}
	}
	return text + ", " + runtime.Version() + " " + runtime.GOOS + "/" + runtime.GOARCH
}
