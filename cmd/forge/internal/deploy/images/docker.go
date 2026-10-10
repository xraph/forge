package images

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

// PrepareDocker preserves named daemon contexts in isolated registry credentials.
func PrepareDocker(ctx context.Context, runner execx.Runner, rootPath string, d *model.Deployment, config string) error {
	if config == "" {
		return nil
	}

	lock, err := state.OpenPrivate(rootPath, "registry-contexts", registryConnectionName(d))
	if err != nil {
		return err
	}
	defer lock.Close()

	release, err := lock.Lock(ctx)
	if err != nil {
		return err
	}
	defer release()

	if err := preparePlugins(config); err != nil {
		return err
	}

	name := d.Target.DockerContext
	if name == "" {
		response, err := runner.Run(ctx, execx.Command{Name: "docker", Args: []string{"context", "show"}, Dir: rootPath})
		if err != nil {
			return errors.New("cannot resolve the selected Docker context")
		}

		name = strings.TrimSpace(response.Stdout)
	}

	if !regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]{0,127}$`).MatchString(name) {
		return errors.New("invalid selected Docker context")
	}

	if name == "default" {
		return nil
	}

	args := []string{"--config", config, "context", "inspect", name}
	if _, err := runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: rootPath}); err != nil {
		exported, err := runner.Run(ctx, execx.Command{Name: "docker", Args: []string{"context", "export", name, "-"}, Dir: rootPath})
		if err != nil {
			return errors.New("cannot export the selected Docker context")
		}

		args = []string{"--config", config, "context", "import", name, "-"}
		if _, err := runner.Run(ctx, execx.Command{Name: "docker", Args: args, Dir: rootPath, Stdin: strings.NewReader(exported.Stdout)}); err != nil {
			return errors.New("cannot prepare the selected private Docker context")
		}
	}

	if d.Target.DockerContext == "" {
		if _, err := runner.Run(ctx, execx.Command{Name: "docker", Args: []string{"--config", config, "context", "use", name}, Dir: rootPath}); err != nil {
			return errors.New("cannot select the private Docker context")
		}
	}

	return nil
}

// DockerEnv keeps configured Buildx builders available without changing global credentials.
func DockerEnv() []string {
	if os.Getenv("BUILDX_CONFIG") != "" {
		return nil
	}

	global := dockerConfigPath()
	if global == "" {
		return nil
	}

	return []string{"BUILDX_CONFIG=" + filepath.Join(global, "buildx")}
}

func preparePlugins(config string) error {
	global := dockerConfigPath()
	if global == "" {
		return nil
	}

	plugins := []string{filepath.Join(global, "cli-plugins")}

	raw, err := os.ReadFile(filepath.Join(global, "config.json"))
	if err == nil {
		var cfg struct {
			Plugins []string `json:"cliPluginsExtraDirs"`
		}
		if json.Unmarshal(raw, &cfg) != nil {
			return errors.New("invalid global Docker plugin configuration")
		}

		plugins = append(plugins, cfg.Plugins...)
	} else if !os.IsNotExist(err) {
		return errors.New("cannot read Docker plugin configuration")
	}

	root, err := os.OpenRoot(config)
	if err != nil {
		return err
	}
	defer root.Close()

	raw, err = root.ReadFile("config.json")
	if err != nil {
		return err
	}

	var cfg map[string]json.RawMessage
	if json.Unmarshal(raw, &cfg) != nil {
		return errors.New("invalid private Docker configuration")
	}

	cfg["cliPluginsExtraDirs"], err = json.Marshal(plugins)
	if err != nil {
		return err
	}

	raw, err = json.Marshal(cfg)
	if err != nil {
		return err
	}

	return privateWrite(root, "config.json", raw)
}
func dockerConfigPath() string {
	if path := os.Getenv("DOCKER_CONFIG"); path != "" {
		return path
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, ".docker")
}
