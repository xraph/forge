package main

import (
	"runtime"
	"time"

	"github.com/xraph/forge/v0/internal/commands"
)

var (
	// Version information (set by build process)
	Version   = "dev"
	GitCommit = "unknown"
	GitBranch = "unknown"
	BuildTime = "unknown"
	GoVersion = runtime.Version()

	// Build information
	BuildUser = "unknown"
	BuildHost = "unknown"
)

// GetVersionInfo returns detailed version information
func GetVersionInfo() commands.VersionInfo {
	buildTime, _ := time.Parse(time.RFC3339, BuildTime)

	return commands.VersionInfo{
		Version:   Version,
		GitCommit: GitCommit,
		GitBranch: GitBranch,
		BuildTime: BuildTime,
		GoVersion: GoVersion,
		BuildUser: BuildUser,
		BuildHost: BuildHost,
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
		Timestamp: buildTime,
	}
}

// IsDevVersion returns true if this is a development version
func IsDevVersion() bool {
	return Version == "dev" || Version == "" || GitCommit == "unknown"
}

// GetShortVersion returns a short version string
func GetShortVersion() string {
	if IsDevVersion() {
		return "dev"
	}
	return Version
}

// GetFullVersion returns a full version string with commit info
func GetFullVersion() string {
	version := GetShortVersion()
	if GitCommit != "unknown" && len(GitCommit) >= 7 {
		version += " (" + GitCommit[:7] + ")"
	}
	return version
}
