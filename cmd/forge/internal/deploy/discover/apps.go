package discover

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/xraph/forge/cmd/forge/config"
)

// apps lists buildable apps from build.apps, or cmd/*, or the workspace globs.
func apps(cfg *config.ForgeConfig) ([]App, error) {
	var out []App

	add := func(name, dir, mainPath, module string) {
		a := App{Name: name, Dir: dir, MainPath: mainPath, Module: module}
		if ac, err := config.LoadAppConfig(dir); err == nil {
			a.Type = ac.App.Type
			a.Port = ac.Dev.GetPort()
		}

		a.ConfigPaths = configPaths(cfg.RootDir, name, dir)
		out = append(out, a)
	}

	if len(cfg.Build.Apps) > 0 {
		for _, b := range cfg.Build.Apps {
			dir := filepath.Join(cfg.RootDir, strings.TrimPrefix(b.Cmd, "./"))
			add(b.Name, dir, strings.TrimPrefix(b.Cmd, "./"), cfg.Project.Module)
		}

		return out, nil
	}

	if cfg.IsSingleModule() {
		cmdDir := filepath.Join(cfg.RootDir, cfg.Project.GetStructure().Cmd)

		entries, err := os.ReadDir(cmdDir)
		if err != nil {
			return nil, err
		}

		for _, e := range entries {
			if !e.IsDir() {
				continue
			}

			dir := filepath.Join(cmdDir, e.Name())
			if _, err := os.Stat(filepath.Join(dir, "main.go")); err != nil {
				continue
			}

			add(e.Name(), dir, filepath.Join(cfg.Project.GetStructure().Cmd, e.Name()), cfg.Project.Module)
		}

		return out, nil
	}

	for _, glob := range []string{cfg.Project.Workspace.Apps, cfg.Project.Workspace.Services} {
		if glob == "" {
			continue
		}

		matches, _ := filepath.Glob(filepath.Join(cfg.RootDir, strings.TrimPrefix(glob, "./")))
		for _, m := range matches {
			main := filepath.Join(m, "cmd", "main.go")

			mainPath := "cmd"

			if _, err := os.Stat(main); err != nil {
				main = filepath.Join(m, "main.go")
				mainPath = "."

				if _, err := os.Stat(main); err != nil {
					continue
				}
			}

			rel, _ := filepath.Rel(cfg.RootDir, m)
			add(filepath.Base(m), filepath.Join(m, mainPath), filepath.Join(rel, mainPath), moduleOf(m, cfg.Project.Module))
		}
	}

	return out, nil
}

func configPaths(root, name, dir string) []string {
	var out []string

	for _, p := range []string{
		filepath.Join(root, "config", name+".yaml"),
		filepath.Join(root, "config", name+".yml"),
		filepath.Join(dir, "config.yaml"),
		filepath.Join(dir, "config.yml"),
		filepath.Join(root, "config.yaml"),
		filepath.Join(root, "config.yml"),
	} {
		if _, err := os.Stat(p); err == nil {
			out = append(out, p)
		}
	}

	return out
}

func moduleOf(dir, fallback string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return fallback
	}

	for line := range strings.SplitSeq(string(data), "\n") {
		if after, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.TrimSpace(after)
		}
	}

	return fallback
}
