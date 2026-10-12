package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveBuildMeta(t *testing.T) {
	unset := buildMeta{Version: "dev", Commit: "unknown", Date: "unknown"}

	tests := []struct {
		name string
		ld   buildMeta
		info *debug.BuildInfo
		want buildMeta
	}{
		{
			name: "goreleaser ldflags win over build info",
			ld:   buildMeta{Version: "1.12.7", Commit: "ca11677", Date: "2026-10-10T22:15:29Z"},
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261011232011-3db9f67b3305"}},
			want: buildMeta{Version: "1.12.7", Commit: "ca11677", Date: "2026-10-10T22:15:29Z"},
		},
		{
			name: "make install git describe drops the v prefix",
			ld:   buildMeta{Version: "v1.12.7-3-g3db9f67", Commit: "3db9f67", Date: "2026-10-11T21:44:00Z"},
			info: &debug.BuildInfo{Main: debug.Module{Version: "(devel)"}},
			want: buildMeta{Version: "1.12.7-3-g3db9f67", Commit: "3db9f67", Date: "2026-10-11T21:44:00Z"},
		},
		{
			name: "go install at latest reports the pseudo-version and its commit",
			ld:   unset,
			info: &debug.BuildInfo{Main: debug.Module{Version: "v0.0.0-20261011232011-3db9f67b3305"}},
			want: buildMeta{Version: "0.0.0-20261011232011-3db9f67b3305", Commit: "3db9f67b3305", Date: "unknown"},
		},
		{
			name: "go install at a tagged module version",
			ld:   unset,
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.13.0"}},
			want: buildMeta{Version: "1.13.0", Commit: "unknown", Date: "unknown"},
		},
		{
			name: "pseudo-version based on a prerelease tag",
			ld:   unset,
			info: &debug.BuildInfo{Main: debug.Module{Version: "v1.12.8-0.20261011232011-3db9f67b3305"}},
			want: buildMeta{Version: "1.12.8-0.20261011232011-3db9f67b3305", Commit: "3db9f67b3305", Date: "unknown"},
		},
		{
			name: "local go build with vcs stamping and a dirty tree",
			ld:   unset,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "v0.0.0-20261011232011-3db9f67b3305+dirty"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "3db9f67b33053f7c0a6d7b4b3c3a1f0e9d8c7b6a"},
					{Key: "vcs.modified", Value: "true"},
				},
			},
			want: buildMeta{Version: "0.0.0-20261011232011-3db9f67b3305+dirty", Commit: "3db9f67b3305-dirty", Date: "unknown"},
		},
		{
			name: "local go build without a module version keeps dev",
			ld:   unset,
			info: &debug.BuildInfo{
				Main: debug.Module{Version: "(devel)"},
				Settings: []debug.BuildSetting{
					{Key: "vcs.revision", Value: "3db9f67b33053f7c0a6d7b4b3c3a1f0e9d8c7b6a"},
					{Key: "vcs.modified", Value: "false"},
				},
			},
			want: buildMeta{Version: "dev", Commit: "3db9f67b3305", Date: "unknown"},
		},
		{
			name: "no build info leaves the defaults alone",
			ld:   unset,
			info: nil,
			want: unset,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveBuildMeta(tt.ld, tt.info)
			if got != tt.want {
				t.Errorf("resolveBuildMeta() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
