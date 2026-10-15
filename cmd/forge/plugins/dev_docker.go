package plugins

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"golang.org/x/mod/modfile"

	"github.com/xraph/forge/cli"
	"github.com/xraph/forge/cmd/forge/config"
)

// dockerAppWatcher manages Docker container lifecycle for dev mode with hot reload.
type dockerAppWatcher struct {
	watcher       *fsnotify.Watcher
	config        *config.ForgeConfig
	app           *AppInfo
	dockerConfig  *config.DockerDevConfig
	containerName string
	containerID   string
	imageName     string
	debouncer     *debouncer
	mu            sync.Mutex
	mainFile      string
	port          int
	running       bool
}

// newDockerAppWatcher creates a Docker-based app watcher for dev mode.
func newDockerAppWatcher(
	cfg *config.ForgeConfig,
	app *AppInfo,
	dockerCfg *config.DockerDevConfig,
	mainFile string,
	port int,
) (*dockerAppWatcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create fsnotify watcher: %w", err)
	}

	// Use delay from config if available, otherwise default to 300ms.
	delay := 300 * time.Millisecond
	if cfg.Dev.HotReload.Enabled && cfg.Dev.HotReload.Delay > 0 {
		delay = cfg.Dev.HotReload.Delay
	}

	containerName := devContainerName(app.Name)
	imageName := "forge-dev-" + app.Name + ":latest"

	dw := &dockerAppWatcher{
		watcher:       watcher,
		config:        cfg,
		app:           app,
		dockerConfig:  dockerCfg,
		containerName: containerName,
		imageName:     imageName,
		debouncer:     newDebouncer(delay),
		mainFile:      mainFile,
		port:          port,
	}

	// Setup watch directories using shared helper.
	if err := setupWatchersForConfig(watcher, cfg, app.Path); err != nil {
		watcher.Close()

		return nil, err
	}

	return dw, nil
}

// Start builds the dev Docker image and starts the container.
func (dw *dockerAppWatcher) Start(ctx cli.CommandContext) error {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	// Stop existing container if running.
	if dw.running {
		dw.stopContainer(ctx.Context())
	}

	// Build dev image.
	ctx.Info(fmt.Sprintf("Building Docker dev image for %s...", dw.app.Name))

	if err := dw.buildDevImage(ctx); err != nil {
		return fmt.Errorf("failed to build dev image: %w", err)
	}

	// Run container.
	ctx.Info(fmt.Sprintf("Starting container %s...", dw.containerName))

	if err := dw.runContainer(ctx); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	dw.running = true
	ctx.Success(fmt.Sprintf("Container %s is running", dw.containerName))

	// Stream logs in background.
	go dw.streamLogs(ctx.Context())

	return nil
}

// Restart restarts the container to pick up bind-mounted source changes.
func (dw *dockerAppWatcher) Restart(ctx cli.CommandContext) error {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	if !dw.running {
		return nil
	}

	ctx.Info(fmt.Sprintf("Restarting container %s...", dw.containerName))

	cmd := dockerCommand(ctx.Context(), "restart", dw.containerName)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("docker restart failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	ctx.Success(fmt.Sprintf("Container %s restarted", dw.containerName))

	return nil
}

// Stop stops and removes the Docker container.
func (dw *dockerAppWatcher) Stop(ctx cli.CommandContext) {
	dw.mu.Lock()
	defer dw.mu.Unlock()

	if !dw.running {
		return
	}

	ctx.Info(fmt.Sprintf("Stopping container %s...", dw.containerName))
	dw.stopContainer(ctx.Context())
	ctx.Success(fmt.Sprintf("Container %s stopped and removed", dw.containerName))
}

// Watch listens for file changes and restarts the container.
func (dw *dockerAppWatcher) Watch(ctx context.Context, cliCtx cli.CommandContext) {
	cliCtx.Success("Watching for changes (Docker mode)... (Ctrl+C to stop)")
	cliCtx.Println("")

	for {
		select {
		case <-ctx.Done():
			return

		case event, ok := <-dw.watcher.Events:
			if !ok {
				return
			}

			if !shouldReloadEvent(event, dw.config, dw.config.RootDir) {
				continue
			}

			dw.debouncer.Debounce(func() {
				cliCtx.Println("")
				cliCtx.Warning("Detected change in " + filepath.Base(event.Name))

				if err := dw.Restart(cliCtx); err != nil {
					cliCtx.Error(fmt.Errorf("restart failed: %w", err))
				}
			})

		case err, ok := <-dw.watcher.Errors:
			if !ok {
				return
			}

			cliCtx.Error(fmt.Errorf("watcher error: %w", err))
		}
	}
}

// Close closes the fsnotify watcher.
func (dw *dockerAppWatcher) Close() error {
	return dw.watcher.Close()
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// buildDevImage builds the Docker image using a custom or generated Dockerfile.
func (dw *dockerAppWatcher) buildDevImage(ctx cli.CommandContext) error {
	var dockerfilePath string

	var cleanup func()

	if dw.dockerConfig.Dockerfile != "" {
		// Use user-provided Dockerfile.
		dockerfilePath = filepath.Join(dw.config.RootDir, dw.dockerConfig.Dockerfile)
		if _, err := os.Stat(dockerfilePath); err != nil {
			return fmt.Errorf("dockerfile not found: %s", dockerfilePath)
		}
	} else {
		// Generate a temporary dev Dockerfile.
		var err error

		dockerfilePath, err = dw.generateDevDockerfile()
		if err != nil {
			return fmt.Errorf("failed to generate dev Dockerfile: %w", err)
		}

		cleanup = func() { os.Remove(dockerfilePath) }
	}

	if cleanup != nil {
		defer cleanup()
	}

	args := []string{"build", "-t", dw.imageName, "-f", dockerfilePath}

	// Add build args.
	for k, v := range dw.dockerConfig.BuildArgs {
		args = append(args, "--build-arg", k+"="+v)
	}

	// Add platform if specified.
	if dw.dockerConfig.Platform != "" {
		args = append(args, "--platform", dw.dockerConfig.Platform)
	}

	args = append(args, dw.config.RootDir)

	cmd := dockerCommand(ctx.Context(), args...)
	cmd.Dir = dw.config.RootDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	ctx.Info("  $ docker " + strings.Join(args, " "))

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}

	return nil
}

// generateDevDockerfile creates a temporary Dockerfile optimized for development.
// It dynamically discovers Forge config files (.forge.yaml, config/, .env*, etc.)
// and includes explicit COPY layers for better Docker build caching.
func (dw *dockerAppWatcher) generateDevDockerfile() (string, error) {
	baseImage := dw.dockerConfig.GetImage()

	// Compute the relative path from the project root to the main file directory.
	relMainFile, err := filepath.Rel(dw.config.RootDir, dw.mainFile)
	if err != nil {
		relMainFile = dw.mainFile
	}

	mainDir := filepath.Dir(relMainFile)

	// go mod download reads the go.mod of every directory replace, so each one
	// has to be in the image before the download layer runs.
	replaceDirs, err := localReplaceDirs(dw.config.RootDir)
	if err != nil {
		return "", err
	}

	// Discover config files that should be explicitly copied for caching.
	configCopies := dw.discoverConfigCopyLines()

	// Build the Dockerfile content.
	var buf strings.Builder

	buf.WriteString("# Auto-generated by forge dev --docker\n")
	buf.WriteString("# This file is temporary and will be deleted after build.\n")
	buf.WriteString("FROM " + baseImage + "\n")
	buf.WriteString("RUN apk add --no-cache git\n")
	buf.WriteString("WORKDIR /app\n")
	buf.WriteString("COPY go.mod go.sum* ./\n")

	for _, dir := range replaceDirs {
		buf.WriteString("COPY " + dir + "/go.mod ./" + dir + "/go.mod\n")
	}

	buf.WriteString("RUN go mod download\n")

	if configCopies != "" {
		buf.WriteString("# Forge config files\n")
		buf.WriteString(configCopies)
	}

	buf.WriteString("COPY . .\n")
	fmt.Fprintf(&buf, "CMD [\"go\", \"run\", \"./%s\"]\n", mainDir)

	// Write to a temporary file in the project root.
	tmpFile, err := os.CreateTemp(dw.config.RootDir, ".forge-dev-Dockerfile-*")
	if err != nil {
		return "", fmt.Errorf("failed to create temp dockerfile: %w", err)
	}

	if _, err := tmpFile.WriteString(buf.String()); err != nil {
		tmpFile.Close()
		os.Remove(tmpFile.Name())

		return "", fmt.Errorf("failed to write dockerfile: %w", err)
	}

	tmpFile.Close()

	return tmpFile.Name(), nil
}

// localReplaceDirs returns the directories the project's go.mod replaces
// modules with, as slash-separated paths relative to rootDir, in go.mod order.
// A directory outside rootDir is an error: neither the build context nor the
// /app bind mount can reach it. A project without a go.mod has none.
func localReplaceDirs(rootDir string) ([]string, error) {
	goModPath := filepath.Join(rootDir, "go.mod")

	data, err := os.ReadFile(goModPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}

	if err != nil {
		return nil, fmt.Errorf("failed to read go.mod: %w", err)
	}

	mf, err := modfile.Parse(goModPath, data, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}

	var dirs, outside []string

	for _, r := range mf.Replace {
		// A replacement with a version is another module, not a directory.
		if r.New.Version != "" {
			continue
		}

		target := r.New.Path
		if !filepath.IsAbs(target) {
			target = filepath.Join(rootDir, target)
		}

		rel, err := filepath.Rel(rootDir, target)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			old := r.Old.Path
			if r.Old.Version != "" {
				old += " " + r.Old.Version
			}

			outside = append(outside, "  "+old+" => "+r.New.Path)

			continue
		}

		dir := filepath.ToSlash(rel)
		if dir != "." && !slices.Contains(dirs, dir) {
			dirs = append(dirs, dir)
		}
	}

	if len(outside) > 0 {
		return nil, fmt.Errorf(
			"go.mod replaces modules with directories outside the project root, "+
				"which the dev image can't see:\n%s\n"+
				"move each module inside the project, or set dev.docker.dockerfile to a Dockerfile of your own",
			strings.Join(outside, "\n"))
	}

	return dirs, nil
}

// discoverConfigCopyLines probes the project filesystem for Forge configuration
// files and returns COPY directives for those that exist.
func (dw *dockerAppWatcher) discoverConfigCopyLines() string {
	rootDir := dw.config.RootDir

	var copies strings.Builder

	// .forge.yaml / .forge.yml — project-level Forge config.
	for _, name := range []string{".forge.yaml", ".forge.yml"} {
		if _, err := os.Stat(filepath.Join(rootDir, name)); err == nil {
			copies.WriteString("COPY " + name + " ./\n")
		}
	}

	// config/ directory (from StructureConfig, default "./config").
	structure := dw.config.Project.GetStructure()
	configDir := strings.TrimPrefix(structure.Config, "./")

	if info, err := os.Stat(filepath.Join(rootDir, configDir)); err == nil && info.IsDir() {
		copies.WriteString("COPY " + configDir + "/ ./" + configDir + "/\n")
	}

	// config.yaml / config.yml / config.local.yaml — app runtime config at root.
	for _, name := range []string{"config.yaml", "config.yml", "config.local.yaml", "config.local.yml"} {
		if _, err := os.Stat(filepath.Join(rootDir, name)); err == nil {
			copies.WriteString("COPY " + name + " ./\n")
		}
	}

	// .env* files — environment variable files at root.
	entries, err := os.ReadDir(rootDir)
	if err == nil {
		for _, entry := range entries {
			if !entry.IsDir() && strings.HasPrefix(entry.Name(), ".env") {
				copies.WriteString("COPY " + entry.Name() + " ./\n")
			}
		}
	}

	// Per-app .forge.yaml files in apps/ directory.
	appsDir := strings.TrimPrefix(structure.Apps, "./")

	if info, err := os.Stat(filepath.Join(rootDir, appsDir)); err == nil && info.IsDir() {
		appEntries, readErr := os.ReadDir(filepath.Join(rootDir, appsDir))
		if readErr == nil {
			for _, appEntry := range appEntries {
				if !appEntry.IsDir() {
					continue
				}

				for _, cfgName := range []string{".forge.yaml", ".forge.yml"} {
					cfgPath := filepath.Join(appsDir, appEntry.Name(), cfgName)
					if _, err := os.Stat(filepath.Join(rootDir, cfgPath)); err == nil {
						copies.WriteString("COPY " + cfgPath + " ./" + cfgPath + "\n")
					}
				}
			}
		}
	}

	// database/ directory — migrations and seeds.
	dbDir := strings.TrimPrefix(structure.Database, "./")

	if info, err := os.Stat(filepath.Join(rootDir, dbDir)); err == nil && info.IsDir() {
		copies.WriteString("COPY " + dbDir + "/ ./" + dbDir + "/\n")
	}

	return copies.String()
}

// runContainer starts the Docker container with bind mounts and port forwarding.
func (dw *dockerAppWatcher) runContainer(ctx cli.CommandContext) error {
	// Remove any stale container from a previous crash.
	removeCmd := dockerCommand(ctx.Context(), "rm", "-f", dw.containerName)
	_ = removeCmd.Run()

	args := []string{
		"run", "-d",
		"--name", dw.containerName,
		"-v", dw.config.RootDir + ":/app",
		"-w", "/app",
	}

	// Port forwarding.
	if dw.port > 0 {
		args = append(args, "-p", fmt.Sprintf("%d:%d", dw.port, dw.port))
	}

	// Network.
	network := dw.dockerConfig.GetNetwork()
	if network != "" && network != "bridge" {
		args = append(args, "--network", network)
	}

	// Standard dev environment variables.
	args = append(args,
		"-e", "FORGE_DEV=true",
		"-e", "FORGE_HOT_RELOAD=true",
		"-e", "FORGE_DOCKER=true",
	)

	if dw.port > 0 {
		args = append(args, "-e", fmt.Sprintf("PORT=%d", dw.port))
	}

	// Extra env vars from config.
	for k, v := range dw.dockerConfig.Env {
		args = append(args, "-e", k+"="+v)
	}

	// Extra volumes from config.
	for hostPath, containerPath := range dw.dockerConfig.Volumes {
		args = append(args, "-v", hostPath+":"+containerPath)
	}

	// Platform.
	if dw.dockerConfig.Platform != "" {
		args = append(args, "--platform", dw.dockerConfig.Platform)
	}

	args = append(args, dw.imageName)

	cmd := dockerCommand(ctx.Context(), args...)
	cmd.Dir = dw.config.RootDir

	ctx.Info("  $ docker " + strings.Join(args, " "))

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker run failed: %s: %w", strings.TrimSpace(string(output)), err)
	}

	dw.containerID = strings.TrimSpace(string(output))

	return nil
}

// stopContainer stops and removes the container.
func (dw *dockerAppWatcher) stopContainer(ctx context.Context) {
	// Stop container gracefully (5 second timeout).
	stopCmd := dockerCommand(ctx, "stop", "-t", "5", dw.containerName)
	_ = stopCmd.Run()

	// Remove container.
	rmCmd := dockerCommand(ctx, "rm", "-f", dw.containerName)
	_ = rmCmd.Run()

	dw.running = false
	dw.containerID = ""
}

// streamLogs streams container stdout/stderr to the terminal.
func (dw *dockerAppWatcher) streamLogs(ctx context.Context) {
	cmd := dockerCommand(ctx, "logs", "-f", dw.containerName)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	// Ignore errors — container may have been removed.
	_ = cmd.Run()
}

// dockerCommand builds a docker CLI invocation. Every argument comes from the
// project's own .forge.yaml and app layout, never from a remote source.
func dockerCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "docker", args...)
}

// devContainerName is the container forge dev --docker runs an app in. The
// observability stack scrapes it by this name.
func devContainerName(app string) string {
	return "forge-dev-" + app
}
