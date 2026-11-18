package plugins

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/cmd/forge/config"
)

// publishArg returns the value of the -p flag in args, or "" without one.
func publishArg(args []string) string {
	if i := slices.Index(args, "-p"); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}

	return ""
}

func TestDockerRunArgsPublishAddress(t *testing.T) {
	tests := []struct {
		name    string
		address string
		want    string
	}{
		{name: "loopback by default", address: "", want: "127.0.0.1:8080:8080"},
		{name: "every interface when configured", address: "0.0.0.0", want: "0.0.0.0:8080:8080"},
		{name: "ipv6 is bracketed", address: "::1", want: "[::1]:8080:8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dw := &dockerAppWatcher{
				config:        &config.ForgeConfig{RootDir: "/src"},
				dockerConfig:  &config.DockerDevConfig{PublishAddress: tt.address},
				containerName: "forge-dev-api",
				imageName:     "forge-dev-api:latest",
				port:          8080,
			}

			assert.Equal(t, tt.want, publishArg(dw.runArgs()))
		})
	}
}

func TestDockerRunArgsNoPortNoPublish(t *testing.T) {
	dw := &dockerAppWatcher{
		config:       &config.ForgeConfig{RootDir: "/src"},
		dockerConfig: &config.DockerDevConfig{},
		imageName:    "forge-dev-api:latest",
	}

	assert.NotContains(t, dw.runArgs(), "-p")
}

func TestDockerRunArgsKeepRealEnvValues(t *testing.T) {
	dw := &dockerAppWatcher{
		config:       &config.ForgeConfig{RootDir: "/src"},
		dockerConfig: &config.DockerDevConfig{Env: map[string]string{"API_TOKEN": "s3cret"}},
		imageName:    "forge-dev-api:latest",
	}

	args := dw.runArgs()
	assert.Contains(t, args, "API_TOKEN=s3cret", "the container must receive the real value")
	assert.Equal(t, "forge-dev-api:latest", args[len(args)-1], "the image is the last argument")

	echoed := strings.Join(maskDockerArgs(args), " ")
	assert.NotContains(t, echoed, "s3cret")
	assert.Contains(t, echoed, "-e API_TOKEN=***")
	assert.Contains(t, args, "API_TOKEN=s3cret", "masking must not modify the exec args")
}

func TestMaskDockerArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "short env flag",
			in:   []string{"run", "-e", "TOKEN=abc", "img"},
			want: []string{"run", "-e", "TOKEN=***", "img"},
		},
		{
			name: "long env flag",
			in:   []string{"run", "--env", "TOKEN=abc"},
			want: []string{"run", "--env", "TOKEN=***"},
		},
		{
			name: "env flag with equals",
			in:   []string{"run", "-e=TOKEN=abc", "--env=PASS=a=b"},
			want: []string{"run", "-e=TOKEN=***", "--env=PASS=***"},
		},
		{
			name: "short env flag with attached value",
			in:   []string{"run", "-eTOKEN=abc"},
			want: []string{"run", "-eTOKEN=***"},
		},
		{
			name: "build args",
			in:   []string{"build", "--build-arg", "NPM_TOKEN=abc", "--build-arg=GH=xyz", "."},
			want: []string{"build", "--build-arg", "NPM_TOKEN=***", "--build-arg=GH=***", "."},
		},
		{
			name: "bare key passes through from the host and has no value",
			in:   []string{"run", "-e", "HOME"},
			want: []string{"run", "-e", "HOME"},
		},
		{
			name: "other flags untouched",
			in:   []string{"run", "-d", "--name", "x", "-v", "/a:/b", "-p", "127.0.0.1:1:1", "--network", "n"},
			want: []string{"run", "-d", "--name", "x", "-v", "/a:/b", "-p", "127.0.0.1:1:1", "--network", "n"},
		},
		{
			name: "trailing flag without a value",
			in:   []string{"run", "-e"},
			want: []string{"run", "-e"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := slices.Clone(tt.in)

			assert.Equal(t, tt.want, maskDockerArgs(tt.in))
			assert.Equal(t, in, tt.in, "the input slice must not be modified")
		})
	}
}

func TestResolveDockerConfigPublishAddress(t *testing.T) {
	plugin := &DevPlugin{config: &config.ForgeConfig{
		Dev: config.DevConfig{Docker: config.DockerDevConfig{PublishAddress: "10.0.0.2"}},
	}}

	got := plugin.resolveDockerConfig(&AppInfo{Name: "api"}, "")
	assert.Equal(t, "10.0.0.2", got.GetPublishAddress(), "the project setting is used without an app override")

	app := &AppInfo{Name: "api", AppConfig: &config.AppConfig{
		Dev: config.AppDevConfig{Docker: &config.DockerDevConfig{PublishAddress: "0.0.0.0"}},
	}}

	got = plugin.resolveDockerConfig(app, "")
	assert.Equal(t, "0.0.0.0", got.GetPublishAddress(), "an app setting overrides the project")

	assert.Equal(t, "127.0.0.1", (&config.DockerDevConfig{}).GetPublishAddress())
}

func TestDevCommandHasPublishAddressFlag(t *testing.T) {
	cmds := (&DevPlugin{}).Commands()
	require.NotEmpty(t, cmds)

	found := false

	for _, flag := range cmds[0].Flags() {
		if flag.Name() == "publish-address" {
			found = true
		}
	}

	assert.True(t, found, "forge dev must take --publish-address")
}
