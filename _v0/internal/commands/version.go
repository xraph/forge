package commands

import (
	"fmt"
	"time"

	"github.com/xraph/forge/v0/pkg/cli"
)

// VersionInfo contains detailed version information
type VersionInfo struct {
	Version   string    `json:"version"`
	GitCommit string    `json:"git_commit"`
	GitBranch string    `json:"git_branch"`
	BuildTime string    `json:"build_time"`
	GoVersion string    `json:"go_version"`
	BuildUser string    `json:"build_user"`
	BuildHost string    `json:"build_host"`
	OS        string    `json:"os"`
	Arch      string    `json:"arch"`
	Timestamp time.Time `json:"timestamp"`
}

func (i VersionInfo) IsDevVersion() bool {
	return i.Version == "dev" || i.Version == "" || i.GitCommit == "unknown"
}

func (i VersionInfo) GetShortVersion() string {
	if i.IsDevVersion() {
		return "dev"
	}
	return i.Version
}

func (i VersionInfo) GetFullVersion() string {
	version := i.GetShortVersion()
	if i.GitCommit != "unknown" && len(i.GitCommit) >= 7 {
		version += " (" + i.GitCommit[:7] + ")"
	}
	return version
}

// VersionCommand creates the 'forge version' command
func VersionCommand(info VersionInfo) *cli.Command {
	return &cli.Command{
		Use:   "version",
		Short: "Show version information",
		Long: `Display version information for the Forge CLI.

Shows the current version, build information, and runtime details.`,
		Example: `  # Show version
  forge version

  # Show short version only
  forge version --short

  # Show version with commit info
  forge version --commit

  # JSON output
  forge version --json`,
		Run: func(ctx cli.CLIContext, args []string) error {
			versionInfo := info

			short := ctx.GetBool("short")
			json := ctx.GetBool("json")
			commit := ctx.GetBool("commit")

			if json {
				return ctx.OutputData(versionInfo)
			}

			if short {
				ctx.Info(info.GetShortVersion())
				return nil
			}

			// Display formatted version information
			ctx.Info(fmt.Sprintf("Forge CLI %s", info.GetFullVersion()))

			if commit || info.IsDevVersion() {
				ctx.Info(fmt.Sprintf("Commit:     %s", versionInfo.GitCommit))
				ctx.Info(fmt.Sprintf("Branch:     %s", versionInfo.GitBranch))
			}

			ctx.Info(fmt.Sprintf("Built:      %s", versionInfo.BuildTime))
			ctx.Info(fmt.Sprintf("Go version: %s", versionInfo.GoVersion))
			ctx.Info(fmt.Sprintf("OS/Arch:    %s/%s", versionInfo.OS, versionInfo.Arch))

			return nil
		},
		Flags: []*cli.FlagDefinition{
			cli.BoolFlag("short", "s", "Show only version number"),
			cli.BoolFlag("json", "", "Output in JSON format"),
			cli.BoolFlag("commit", "c", "Show commit information"),
		},
	}
}
