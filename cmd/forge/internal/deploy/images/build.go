// Package images builds and verifies immutable service images for deployment adapters.
package images

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/plan"
	"github.com/xraph/forge/cmd/forge/internal/deploy/state"
)

var immutableDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func Ref(image model.Image) string {
	if image.Digest != "" {
		return image.Repository + "@" + image.Digest
	}

	if immutableDigest.MatchString(image.Repository) {
		return image.Repository
	}

	if image.Tag != "" {
		return image.Repository + ":" + image.Tag
	}

	return image.Repository + ":latest"
}
func dockerArgs(d *model.Deployment, config string, args ...string) []string {
	prefix := []string{}
	if config != "" {
		prefix = append(prefix, "--config", config)
	}

	if d.Target.DockerContext != "" {
		prefix = append(prefix, "--context", d.Target.DockerContext)
	}

	return append(prefix, args...)
}
func registryConfig(ctx context.Context, runner execx.Runner, root string, d *model.Deployment, values map[string]string) (string, error) {
	registry := d.Target.Build.Registry
	name := registryConnectionName(d)

	if registry.SecretRef != "" {
		token := values[registry.SecretRef]
		if token == "" {
			token = values[strings.ToUpper(strings.NewReplacer("-", "_", ".", "_").Replace(d.Project+"_"+registry.SecretRef))]
		}

		if token != "" {
			if err := Connect(ctx, runner, root, name, registry.Host, registry.Username, token); err != nil {
				return "", err
			}
		}
	}

	if name == "" {
		return "", nil
	}

	return ConnectionDir(root, name, registry.Host)
}
func localImage(ctx context.Context, runner execx.Runner, root string, d *model.Deployment, config string, ref string) (model.Image, error) {
	res, err := runner.Run(ctx, execx.Command{Name: "docker", Args: dockerArgs(d, config, "image", "inspect", "--format", "{{.Id}}", ref), Dir: root, Env: DockerEnv()})
	if err != nil {
		return model.Image{}, errors.New("cannot verify image in the selected Docker store")
	}

	id := strings.TrimSpace(res.Stdout)
	if !immutableDigest.MatchString(id) {
		return model.Image{}, errors.New("docker returned an invalid image identity")
	}

	return model.Image{Repository: id}, nil
}
func registryImage(ctx context.Context, runner execx.Runner, root string, d *model.Deployment, config string, image model.Image) (model.Image, error) {
	res, err := runner.Run(ctx, execx.Command{Name: "docker", Args: dockerArgs(d, config, "buildx", "imagetools", "inspect", Ref(image), "--format", "{{json .Manifest}}"), Dir: root, Env: DockerEnv()})
	if err != nil {
		return model.Image{}, errors.New("registry image verification failed; check pull access")
	}

	var manifest struct {
		Digest string `json:"digest"`
	}
	if err := json.Unmarshal([]byte(res.Stdout), &manifest); err != nil {
		manifest.Digest = strings.TrimSpace(res.Stdout)
	}

	if !immutableDigest.MatchString(manifest.Digest) {
		return model.Image{}, errors.New("registry returned an invalid immutable digest")
	}

	if image.Digest != "" && image.Digest != manifest.Digest {
		return model.Image{}, errors.New("registry digest differs from the approved image")
	}

	image.Digest = manifest.Digest
	image.Tag = ""
	image.Dockerfile = ""

	return image, nil
}
func remember(st *state.Store, p *plan.Plan, name string, image model.Image) error {
	raw, err := json.Marshal(image)
	if err != nil {
		return err
	}

	if err := st.WriteFile("image-"+name+"-"+p.Hash+".json", raw); err != nil {
		return err
	}

	return st.Journal().Record(state.Event{Time: time.Now().UTC(), Op: "image:" + name, Status: state.StatusAccepted, Message: "Immutable image verified", ProviderID: Ref(image), IdempotencyKey: p.Hash + ":image:" + name})
}

// Build verifies all selected images before any backend, migration or workload changes.
func Build(ctx context.Context, runner execx.Runner, root string, p *plan.Plan, st *state.Store, values map[string]string) (map[string]model.Image, error) {
	d := p.Deployment

	build := d.Target.Build
	if build.Source == "git" {
		return nil, errors.New("provider Git builds use the provider source adapter")
	}

	if build.Delivery != "registry" && len(build.Platforms) > 1 {
		return nil, errors.New("multiple platforms require registry publication")
	}

	if build.Builder == "host" && len(build.Platforms) > 1 {
		return nil, errors.New("host builds support one platform")
	}

	if build.Source == "remote" && build.Builder == "" {
		return nil, errors.New("remote builds require a named Buildx builder")
	}

	for _, svc := range d.Services {
		if !connectionName.MatchString(svc.Name) {
			return nil, errors.New("invalid service name")
		}

		if (build.Source == "existing" || build.Source == "ci") && !immutableDigest.MatchString(svc.Image.Digest) {
			return nil, errors.New("existing images require an immutable sha256 digest")
		}
	}

	if _, err := st.Journal().Events(); err != nil {
		return nil, err
	}

	config, err := registryConfig(ctx, runner, root, d, values)
	if err != nil {
		return nil, err
	}

	if err := PrepareDocker(ctx, runner, root, d, config); err != nil {
		return nil, err
	}

	source := ""
	if build.Source != "existing" && build.Source != "ci" {
		source, err = buildContext(ctx, root, d, st, p)
		if err != nil {
			return nil, err
		}
	}

	result := map[string]model.Image{}

	for _, svc := range d.Services {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		// Resume uses the first verified image, including partially failed rollouts.
		saved, readErr := st.ReadFile("image-" + svc.Name + "-" + p.Hash + ".json")
		if readErr == nil {
			var image model.Image
			if json.Unmarshal(saved, &image) != nil || image.Digest == "" && !immutableDigest.MatchString(image.Repository) || image.Digest != "" && !immutableDigest.MatchString(image.Digest) {
				return nil, errors.New("saved image identity is invalid")
			}

			if image.Digest != "" {
				image, err = registryImage(ctx, runner, root, d, config, image)
				if err == nil && (d.Target.Provider == "compose" || build.Delivery != "registry") {
					err = pull(ctx, runner, root, d, config, image)
				}
			} else {
				image, err = localImage(ctx, runner, root, d, config, Ref(image))
			}

			if err != nil {
				return nil, err
			}

			result[svc.Name] = image

			continue
		}

		if !os.IsNotExist(readErr) {
			return nil, readErr
		}

		image := svc.Image
		if build.Source == "existing" || build.Source == "ci" {
			image, err = registryImage(ctx, runner, root, d, config, image)
			if err == nil && (build.Delivery != "registry" || d.Target.Provider == "compose") {
				err = pull(ctx, runner, root, d, config, image)
			}
		} else {
			image, err = buildOne(ctx, runner, root, source, p, st, config, svc)
			if err == nil && image.Digest != "" && d.Target.Provider == "compose" {
				err = pull(ctx, runner, root, d, config, image)
			}
		}

		if err != nil {
			return nil, err
		}

		if err := remember(st, p, svc.Name, image); err != nil {
			return nil, err
		}

		result[svc.Name] = image
	}

	return result, nil
}
func buildOne(ctx context.Context, runner execx.Runner, root, source string, p *plan.Plan, st *state.Store, config string, svc model.Service) (model.Image, error) {
	d := p.Deployment
	contextPath := source
	file := ""

	if d.Target.Build.Builder == "host" {
		module, pkg, err := moduleFor(source, svc.MainPath)
		if err != nil {
			return model.Image{}, err
		}

		arch := runtime.GOARCH

		if len(d.Target.Build.Platforms) > 0 {
			parts := strings.Split(d.Target.Build.Platforms[0], "/")
			if len(parts) != 2 || parts[0] != "linux" {
				return model.Image{}, errors.New("host builds require a Linux platform")
			}

			arch = parts[1]
		}

		if err := st.MkdirAll(filepath.Join("build", svc.Name)); err != nil {
			return model.Image{}, err
		}

		contextPath = filepath.Join(st.Dir(), "build", svc.Name)

		work := "off"
		if _, err := os.Stat(filepath.Join(source, "go.work")); err == nil {
			work = filepath.Join(source, "go.work")
		}

		command := execx.Command{Name: "go", Args: []string{"build", "-mod=readonly", "-trimpath", "-ldflags", "-s -w", "-o", filepath.Join(contextPath, "app"), pkg}, Dir: filepath.Join(source, module), Env: []string{"GOWORK=" + work, "GOOS=linux", "GOARCH=" + arch, "CGO_ENABLED=0"}}
		if _, err := runner.Run(ctx, command); err != nil {
			return model.Image{}, errors.New("host Go build failed")
		}
	} else if svc.Image.Dockerfile != "" {
		if !filepath.IsLocal(svc.Image.Dockerfile) {
			return model.Image{}, errors.New("Dockerfile must be inside the project")
		}

		file = filepath.Join(source, svc.Image.Dockerfile)
		if _, err := os.Stat(file); err != nil {
			return model.Image{}, errors.New("custom Dockerfile is unavailable in the filtered context")
		}
	}

	if file == "" {
		raw, err := Dockerfile(d, svc, source)
		if err != nil {
			return model.Image{}, err
		}

		relative := filepath.Join("build", svc.Name, "Dockerfile")
		if err := st.MkdirAll(filepath.Dir(relative)); err != nil {
			return model.Image{}, err
		}

		if err := st.WriteFile(relative, raw); err != nil {
			return model.Image{}, err
		}

		file = filepath.Join(st.Dir(), relative)
	}

	args := []string{"buildx", "build", "-t", Ref(svc.Image), "-f", file}
	if d.Target.Build.Source == "remote" {
		args = append(args, "--builder", d.Target.Build.Builder)
	}

	if len(d.Target.Build.Platforms) > 0 {
		args = append(args, "--platform", strings.Join(d.Target.Build.Platforms, ","))
	}

	directPush := d.Target.Build.Delivery == "registry" && (d.Target.Build.Source == "remote" || len(d.Target.Build.Platforms) > 1)
	if directPush {
		args = append(args, "--push")
	} else {
		args = append(args, "--load")
	}

	args = append(args, contextPath)
	if _, err := runner.Run(ctx, execx.Command{Name: "docker", Args: dockerArgs(d, config, args...), Dir: root, Env: DockerEnv()}); err != nil {
		return model.Image{}, errors.New("docker image build failed")
	}

	if d.Target.Build.Delivery == "registry" {
		if !directPush {
			if _, err := runner.Run(ctx, execx.Command{Name: "docker", Args: dockerArgs(d, config, "push", Ref(svc.Image)), Dir: root, Env: DockerEnv()}); err != nil {
				return model.Image{}, errors.New("registry image publication failed; check push access")
			}
		}

		return registryImage(ctx, runner, root, d, config, svc.Image)
	}

	return localImage(ctx, runner, root, d, config, Ref(svc.Image))
}

func pull(ctx context.Context, runner execx.Runner, root string, d *model.Deployment, config string, image model.Image) error {
	if _, err := runner.Run(ctx, execx.Command{Name: "docker", Args: dockerArgs(d, config, "pull", Ref(image)), Dir: root, Env: DockerEnv()}); err != nil {
		return errors.New("target image pull failed")
	}

	_, err := localImage(ctx, runner, root, d, config, Ref(image))

	return err
}

func registryConnectionName(d *model.Deployment) string {
	if d.Target.Build.Registry.Auth != "" {
		return d.Target.Build.Registry.Auth
	}

	name := d.Target.Build.Registry.SecretRef
	if name != "" && !connectionName.MatchString(name) {
		hash := sha256.Sum256([]byte(name))
		name = "registry-" + hex.EncodeToString(hash[:])[:16]
	}

	return name
}

// LocalIdentity verifies a pulled registry image in the selected Docker store.
func LocalIdentity(ctx context.Context, runner execx.Runner, root string, d *model.Deployment, image model.Image) (model.Image, error) {
	config, err := ConfigDir(root, d)
	if err != nil {
		return model.Image{}, err
	}

	return localImage(ctx, runner, root, d, config, Ref(image))
}

// ConfigDir supplies the same authenticated connection to deployment pulls.
func ConfigDir(root string, d *model.Deployment) (string, error) {
	name := registryConnectionName(d)
	if name == "" {
		return "", nil
	}

	return ConnectionDir(root, name, d.Target.Build.Registry.Host)
}
