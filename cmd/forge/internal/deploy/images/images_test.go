package images

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

const testDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

type imageRunner struct {
	*execx.Fake

	run func(execx.Command) (execx.Result, error)
}

func (f *imageRunner) Run(_ context.Context, c execx.Command) (execx.Result, error) {
	f.Calls = append(f.Calls, c)

	return f.run(c)
}
func fakeRunner(t *testing.T, run func(execx.Command) (execx.Result, error)) *imageRunner {
	t.Helper()

	return &imageRunner{Fake: execx.NewFake(t), run: run}
}
func put(t *testing.T, root, path, value string) {
	t.Helper()

	name := filepath.Join(root, path)
	if err := os.MkdirAll(filepath.Dir(name), 0700); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(name, []byte(value), 0600); err != nil {
		t.Fatal(err)
	}
}
func imageFixture(t *testing.T) (string, *plan.Plan, *state.Store) {
	t.Helper()
	root := t.TempDir()
	put(t, root, "go.mod", "module example.test/app\n\ngo 1.26.0\n")
	put(t, root, "cmd/api/main.go", "package main\nfunc main(){}\n")

	d := &model.Deployment{Project: "atlas", TargetName: "local", Environment: "dev", Target: spec.Target{Provider: "compose"}, Services: []model.Service{{Name: "api", MainPath: "cmd/api", Image: model.Image{Repository: "atlas/api", Tag: "reviewed"}}}}
	p := &plan.Plan{Hash: strings.Repeat("b", 64), Deployment: d}

	st, err := state.Open(root, "local", "dev")
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = st.Close() })

	return root, p, st
}

func TestRegistryLoginPersistsPrivateAuthWithoutTokenArguments(t *testing.T) {
	root := t.TempDir()
	token := "private-registry-token"

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if !slices.Contains(c.Args, "--password-stdin") || strings.Contains(c.String(), token) {
			t.Fatal("unsafe login", c.String())
		}

		raw, err := io.ReadAll(c.Stdin)
		if err != nil || string(raw) != token {
			t.Fatal("missing stdin credential")
		}

		config := c.Args[1]
		put(t, config, "config.json", `{"auths":{"ghcr.io":{"auth":"`+base64.StdEncoding.EncodeToString([]byte("rex:"+token))+`"}}}`)

		return execx.Result{}, nil
	})
	if err := Connect(context.Background(), f, root, "ghcr", "ghcr.io", "rex", token); err != nil {
		t.Fatal(err)
	}

	dir, err := ConnectionDir(root, "ghcr", "ghcr.io")
	if err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private config absent", err)
	}

	if strings.Contains(dir, token) {
		t.Fatal("secret path")
	}
}
func TestRegistryLoginRejectsUnpersistedAndLeakingFailures(t *testing.T) {
	for _, denied := range []bool{false, true} {
		f := fakeRunner(t, func(execx.Command) (execx.Result, error) {
			if denied {
				return execx.Result{}, errors.New("private-registry-token")
			}

			return execx.Result{}, nil
		})
		root := t.TempDir()

		err := Connect(context.Background(), f, root, "ghcr", "ghcr.io", "rex", "private-registry-token")
		if err == nil || strings.Contains(err.Error(), "private-registry-token") {
			t.Fatal("unsafe success/error", err)
		}

		if _, err := ConnectionDir(root, "ghcr", "ghcr.io"); err == nil {
			t.Fatal("failed connection persisted")
		}
	}
}
func TestBuildVerifiesExistingImagesWithoutBuilding(t *testing.T) {
	root, p, st := imageFixture(t)
	p.Deployment.Target.Build.Source = "existing"
	p.Deployment.Services[0].Image = model.Image{Repository: "ghcr.io/rex/api", Digest: testDigest}
	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "build") || slices.Contains(c.Args, "push") {
			t.Fatal("existing image rebuilt", c)
		}

		return execx.Result{Stdout: testDigest}, nil
	})

	result, err := Build(context.Background(), f, root, p, st, nil)
	if err != nil || result["api"].Digest != testDigest {
		t.Fatal(result, err)
	}

	if len(f.Calls) == 0 {
		t.Fatal("image never verified")
	}
}
func TestBuildRejectsMalformedDigestsAndUnpublishablePlatforms(t *testing.T) {
	for _, variant := range []string{"existing", "local", "remote-load"} {
		root, p, st := imageFixture(t)
		if variant == "existing" {
			p.Deployment.Target.Build.Source = "existing"
			p.Deployment.Services[0].Image.Digest = "sha256:broken"
		}

		if variant == "remote-load" {
			p.Deployment.Target.Build = spec.Build{Source: "remote", Builder: "builder", Platforms: []string{"linux/amd64", "linux/arm64"}}
		}

		f := fakeRunner(t, func(execx.Command) (execx.Result, error) { return execx.Result{Stdout: "sha256:broken"}, nil })
		if _, err := Build(context.Background(), f, root, p, st, nil); err == nil {
			t.Fatal("invalid image/platform accepted", variant)
		}
	}
}
func TestBuildFailsOnDeniedPushAndNeverJournalsCredentials(t *testing.T) {
	root, p, st := imageFixture(t)
	p.Deployment.Target.Build.Delivery = "registry"
	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "push") {
			return execx.Result{}, errors.New("hidden-token")
		}

		return execx.Result{Stdout: testDigest}, nil
	})

	_, err := Build(context.Background(), f, root, p, st, map[string]string{"TOKEN": "hidden-token"})
	if err == nil || strings.Contains(err.Error(), "hidden-token") {
		t.Fatal("publication denial leaked/ignored", err)
	}

	evs, err := st.Journal().Events()
	if err != nil {
		t.Fatal(err)
	}

	for _, ev := range evs {
		if strings.Contains(ev.Message, "hidden-token") || strings.Contains(ev.ProviderID, "hidden-token") {
			t.Fatal("journal secret")
		}
	}
}
func TestHostBuildUsesNestedModuleAndFreezesImageBeforeRollout(t *testing.T) {
	root, p, st := imageFixture(t)
	put(t, root, "services/api/go.mod", "module example.test/nested\n\ngo 1.26.0\n")
	put(t, root, "services/api/cmd/main.go", "package main\nfunc main(){}\n")

	p.Deployment.Services[0].MainPath = "services/api/cmd"
	p.Deployment.Target.Build.Builder = "host"
	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if c.Name == "go" {
			if !strings.HasSuffix(c.Dir, filepath.Join("services", "api")) || c.Args[len(c.Args)-1] != "./cmd" {
				t.Fatal("wrong module build", c)
			}

			index := slices.Index(c.Args, "-o")
			put(t, filepath.Dir(c.Args[index+1]), "app", "binary")
		}

		return execx.Result{Stdout: testDigest}, nil
	})

	result, err := Build(context.Background(), f, root, p, st, nil)
	if err != nil || Ref(result["api"]) != testDigest {
		t.Fatal(result, err)
	}

	events, err := st.Journal().Events()
	if err != nil {
		t.Fatal(err)
	}

	frozen := false

	for _, event := range events {
		if event.Op == "image:api" && event.ProviderID == testDigest {
			frozen = true
		}
	}

	if !frozen {
		t.Fatal("immutable image not journaled")
	}
}
func TestBuildContextKeepsGoConfigPackagesAndExcludesConfiguredSecretFiles(t *testing.T) {
	root, p, st := imageFixture(t)
	put(t, root, "internal/config/defaults.go", "package config\n")
	put(t, root, "runtime/settings.toml", "secret-value")
	put(t, root, ".env.production", "secret-value")

	p.Deployment.BuildExcludes = []string{"runtime/settings.toml"}
	built := false

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "build") {
			built = true

			source := c.Args[len(c.Args)-1]
			if _, err := os.Stat(filepath.Join(source, "internal/config/defaults.go")); err != nil {
				t.Fatal("Go config package removed", err)
			}

			for _, path := range []string{"runtime/settings.toml", ".env.production", ".forge"} {
				if _, err := os.Stat(filepath.Join(source, path)); !os.IsNotExist(err) {
					t.Fatal("sensitive file copied", path)
				}
			}
		}

		return execx.Result{Stdout: testDigest}, nil
	})
	if _, err := Build(context.Background(), f, root, p, st, nil); err != nil {
		t.Fatal(err)
	}

	if !built {
		t.Fatal("no image built from filtered context")
	}
}

func TestRegistryPublicationUsesBuildxPushForMultiplePlatforms(t *testing.T) {
	root, p, st := imageFixture(t)
	p.Deployment.Target.Build = spec.Build{Delivery: "registry", Platforms: []string{"linux/amd64", "linux/arm64"}}

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "build") && (!slices.Contains(c.Args, "--push") || slices.Contains(c.Args, "--load")) {
			t.Fatal("multi-platform publication requires push", c.Args)
		}

		return execx.Result{Stdout: testDigest}, nil
	})
	if _, err := Build(context.Background(), f, root, p, st, nil); err != nil {
		t.Fatal(err)
	}
}
func TestResumeNeverRebuildsAnImageAfterMigrationFailure(t *testing.T) {
	root, p, st := imageFixture(t)

	f := fakeRunner(t, func(execx.Command) (execx.Result, error) { return execx.Result{Stdout: testDigest}, nil })
	if _, err := Build(context.Background(), f, root, p, st, nil); err != nil {
		t.Fatal(err)
	}

	f.Calls = nil

	result, err := Build(context.Background(), f, root, p, st, nil)
	if err != nil || Ref(result["api"]) != testDigest {
		t.Fatal(err, result)
	}

	for _, call := range f.Calls {
		if slices.Contains(call.Args, "build") {
			t.Fatal("reviewed image rebuilt on resume")
		}
	}
}
func TestBuildRejectsGoWorkspaceAndReplacementOutsideProject(t *testing.T) {
	for _, file := range []string{"go.work", "go.mod"} {
		root, p, st := imageFixture(t)
		if file == "go.work" {
			put(t, root, file, "go 1.26.0\nuse (\n .\n ../outside\n)\n")
		} else {
			put(t, root, file, "module example.test/app\ngo 1.26.0\nreplace example.test/other => ../outside\n")
		}

		f := fakeRunner(t, func(execx.Command) (execx.Result, error) {
			t.Fatal("unsafe context reached builder")

			return execx.Result{}, nil
		})
		if _, err := Build(context.Background(), f, root, p, st, nil); err == nil {
			t.Fatal("outside workspace accepted", file)
		}
	}
}

func TestBuildRejectsSourceChangedAfterPlanApproval(t *testing.T) {
	root, p, st := imageFixture(t)
	p.Inputs = map[string]string{"cmd/api/main.go": strings.Repeat("0", 64)}

	f := fakeRunner(t, func(execx.Command) (execx.Result, error) {
		t.Fatal("changed source reached builder")

		return execx.Result{}, nil
	})
	if _, err := Build(context.Background(), f, root, p, st, nil); err == nil {
		t.Fatal("source changed after approval")
	}
}
func TestRegistryConnectionCannotEscapeTheProject(t *testing.T) {
	root := t.TempDir()

	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, ".forge")); err != nil {
		t.Fatal(err)
	}

	f := fakeRunner(t, func(execx.Command) (execx.Result, error) {
		t.Fatal("outside connection reached login")

		return execx.Result{}, nil
	})
	if err := Connect(context.Background(), f, root, "ghcr", "ghcr.io", "rex", "token"); err == nil {
		t.Fatal("connection escaped project")
	}

	entries, err := os.ReadDir(outside)
	if err != nil || len(entries) != 0 {
		t.Fatal("outside files changed", err)
	}
}
func TestComposeRegistryImagesArePulledBeforeMigration(t *testing.T) {
	root, p, st := imageFixture(t)
	p.Deployment.Target.Build = spec.Build{Source: "remote", Builder: "builder", Delivery: "registry"}
	pulled := false

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "pull") {
			pulled = true
		}

		return execx.Result{Stdout: testDigest}, nil
	})
	if _, err := Build(context.Background(), f, root, p, st, nil); err != nil {
		t.Fatal(err)
	}

	if !pulled {
		t.Fatal("registry image unavailable on Compose host")
	}
}

func TestDockerfileUsesWorkspaceGoVersionAndNestedModule(t *testing.T) {
	root, p, _ := imageFixture(t)
	put(t, root, "go.work", "go 1.26.0\nuse (\n .\n services/api\n)\n")
	put(t, root, "services/api/go.mod", "module example.test/nested\ngo 1.24.0\n")
	put(t, root, "services/api/cmd/main.go", "package main\nfunc main(){}\n")

	svc := p.Deployment.Services[0]
	svc.MainPath = "services/api/cmd"

	raw, err := Dockerfile(p.Deployment, svc, root)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(string(raw), "FROM golang:1.26-alpine") || !strings.Contains(string(raw), "WORKDIR /src/services/api") || !strings.Contains(string(raw), `"./cmd"`) {
		t.Fatal("workspace module builder is incompatible", string(raw))
	}
}

func TestPrivateRegistryConfigPreservesTheSelectedDockerContext(t *testing.T) {
	root, p, st := imageFixture(t)

	login := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		put(t, c.Args[1], "config.json", `{"auths":{"ghcr.io":{"auth":"cmV4OnRva2Vu"}}}`)

		return execx.Result{}, nil
	})
	if err := Connect(context.Background(), login, root, "ghcr", "ghcr.io", "rex", "token"); err != nil {
		t.Fatal(err)
	}

	p.Deployment.Target.Build.Registry = spec.Registry{Host: "ghcr.io", Auth: "ghcr"}
	p.Deployment.Target.DockerContext = "remote-host"
	imported := false

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "context") && slices.Contains(c.Args, "export") {
			return execx.Result{Stdout: "context archive"}, nil
		}

		if slices.Contains(c.Args, "context") && slices.Contains(c.Args, "inspect") {
			return execx.Result{}, errors.New("missing private context")
		}

		if slices.Contains(c.Args, "context") && slices.Contains(c.Args, "import") {
			raw, _ := io.ReadAll(c.Stdin)
			if string(raw) != "context archive" {
				t.Fatal("context not passed through stdin")
			}

			imported = true
		}

		if slices.Contains(c.Args, "build") && !imported {
			t.Fatal("selected context lost in isolated registry config")
		}

		return execx.Result{Stdout: testDigest}, nil
	})
	if _, err := Build(context.Background(), f, root, p, st, nil); err != nil {
		t.Fatal(err)
	}

	if !imported {
		t.Fatal("selected Docker context was not preserved")
	}
}

func TestBuildContextPreservesExecutableAssets(t *testing.T) {
	root, p, st := imageFixture(t)
	put(t, root, "scripts/start.sh", "#!/bin/sh\nexit 0\n")

	if err := os.Chmod(filepath.Join(root, "scripts/start.sh"), 0755); err != nil {
		t.Fatal(err)
	}

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "build") {
			info, err := os.Stat(filepath.Join(c.Args[len(c.Args)-1], "scripts/start.sh"))
			if err != nil || info.Mode().Perm() != 0755 {
				t.Fatal("executable asset mode lost", err)
			}
		}

		return execx.Result{Stdout: testDigest}, nil
	})
	if _, err := Build(context.Background(), f, root, p, st, nil); err != nil {
		t.Fatal(err)
	}
}

func TestPrivateRegistryConfigPreservesPluginsWithoutCopyingGlobalAuth(t *testing.T) {
	root, p, _ := imageFixture(t)
	global := t.TempDir()
	t.Setenv("DOCKER_CONFIG", global)
	put(t, global, "config.json", `{"cliPluginsExtraDirs":["/custom/plugins"],"credsStore":"desktop","auths":{"other.io":{"auth":"global-secret"}}}`)

	login := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		put(t, c.Args[1], "config.json", `{"auths":{"ghcr.io":{"auth":"private-auth"}}}`)

		return execx.Result{}, nil
	})
	if err := Connect(context.Background(), login, root, "ghcr", "ghcr.io", "rex", "token"); err != nil {
		t.Fatal(err)
	}

	p.Deployment.Target.Build.Registry = spec.Registry{Host: "ghcr.io", Auth: "ghcr"}
	p.Deployment.Target.DockerContext = "default"

	config, err := ConfigDir(root, p.Deployment)
	if err != nil {
		t.Fatal(err)
	}

	if err := PrepareDocker(context.Background(), login, root, p.Deployment, config); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(config, "config.json"))
	if err != nil {
		t.Fatal(err)
	}

	var cfg struct {
		Plugins []string `json:"cliPluginsExtraDirs"`
	}
	if json.Unmarshal(raw, &cfg) != nil || !slices.Contains(cfg.Plugins, filepath.Join(global, "cli-plugins")) || !slices.Contains(cfg.Plugins, "/custom/plugins") {
		t.Fatal("installed plugins lost", string(raw))
	}

	if strings.Contains(string(raw), "global-secret") || strings.Contains(string(raw), "credsStore") {
		t.Fatal("global credentials copied")
	}
}

func TestVerifyFrozenImageDoesNotBuildChangedSource(t *testing.T) {
	root, p, _ := imageFixture(t)
	put(t, root, "cmd/api/main.go", "package main\nfunc main(){panic(\"changed source\")}\n")

	f := fakeRunner(t, func(c execx.Command) (execx.Result, error) {
		if slices.Contains(c.Args, "build") {
			t.Fatal("verification rebuilt source")
		}

		return execx.Result{Stdout: testDigest}, nil
	})
	if err := Verify(context.Background(), f, root, p.Deployment, model.Image{Repository: testDigest}); err != nil {
		t.Fatal(err)
	}
}
