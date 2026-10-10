package kubernetes

import (
	"encoding/json"
	"net/url"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/render"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
	"gopkg.in/yaml.v3"
)

var registryDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

func validateGitOps(d *model.Deployment) output.Diagnostics {
	var ds output.Diagnostics
	if d.Target.Release.Mode != "gitops" {
		return ds
	}

	fail := func(msg string) {
		ds = append(ds, output.Diagnostic{Code: output.CodeUnsupportedCommand, Severity: output.SeverityError, Message: msg, Field: "release"})
	}
	release := d.Target.Release

	repo, err := url.Parse(release.Repo)
	if err != nil || repo.Scheme != "https" || repo.Host == "" || repo.User != nil || repo.RawQuery != "" || repo.Fragment != "" {
		fail("GitOps handoff requires a credential-free HTTPS repository URL")
	}

	if !filepath.IsLocal(release.Path) || release.Path == "." || filepath.Clean(release.Path) != release.Path || strings.ContainsAny(release.Path, "\\\r\n") {
		fail("GitOps artifact path must be a clean relative repository directory")
	}

	switch release.Controller {
	case "", "generic", "argo-cd", "flux":
	default:
		fail("choose argo-cd, flux or generic for the controller handoff")
	}

	if release.Approval != "" && release.Approval != "manual" {
		fail("GitOps handoff supports manual controller approval only")
	}

	if d.Target.Build.Delivery != "registry" || d.Target.Build.Source != "existing" && d.Target.Build.Source != "ci" {
		fail("GitOps handoff requires immutable existing or CI registry images")
	}

	if d.Target.Build.Registry.Visibility == "private" && d.Target.Build.Registry.PullSecret == "" {
		fail("GitOps private images require an existing pull Secret")
	}

	for _, s := range d.Services {
		if !registryDigest.MatchString(s.Image.Digest) {
			fail("GitOps service needs an immutable registry image: " + s.Name)
		}

		if len(s.Migrate) > 0 || s.Kind == spec.KindJob {
			fail("GitOps migration and one-off job ordering needs controller qualification; use the direct adapter")
		}
	}

	if len(d.Migrations) > 0 {
		fail("GitOps handoff cannot order migrations without a qualified controller recipe")
	}

	for _, r := range d.Resources {
		if r.Lifecycle != spec.LifecycleExternal {
			fail("GitOps resources must already be provisioned externally: " + r.Name)
		}
	}

	return ds
}

type requiredSecret struct {
	Name string   `json:"name"`
	Keys []string `json:"keys"`
}

func gitOpsHandoff(d *model.Deployment, b *render.Bundle) error {
	if d.Target.Release.Mode != "gitops" {
		return nil
	}

	refs := map[string]map[string]bool{}

	var scan func(any)

	scan = func(v any) {
		switch item := v.(type) {
		case map[string]any:
			if ref, ok := item["secretKeyRef"].(map[string]any); ok {
				name, _ := ref["name"].(string)

				key, _ := ref["key"].(string)
				if name != "" && key != "" {
					if refs[name] == nil {
						refs[name] = map[string]bool{}
					}

					refs[name][key] = true
				}
			}

			for _, child := range item {
				scan(child)
			}
		case []any:
			for _, child := range item {
				scan(child)
			}
		}
	}

	for _, f := range b.Sorted() {
		if !strings.HasSuffix(f.Path, ".yaml") {
			continue
		}

		var obj map[string]any
		if err := yaml.Unmarshal(f.Content, &obj); err != nil {
			return err
		}

		scan(obj)
	}

	names := make([]string, 0, len(refs))
	for name := range refs {
		names = append(names, name)
	}

	sort.Strings(names)

	secrets := []requiredSecret{}

	for _, name := range names {
		keys := make([]string, 0, len(refs[name]))
		for key := range refs[name] {
			keys = append(keys, key)
		}

		sort.Strings(keys)
		secrets = append(secrets, requiredSecret{Name: name, Keys: keys})
	}

	controller := d.Target.Release.Controller
	if controller == "" {
		controller = "generic"
	}

	handoff := struct {
		Schema        string           `json:"schema"`
		Repository    string           `json:"repository"`
		Branch        string           `json:"branch"`
		Path          string           `json:"path"`
		Controller    string           `json:"controller"`
		Context       string           `json:"context"`
		Namespace     string           `json:"namespace"`
		Prune         bool             `json:"prune"`
		AutomaticSync bool             `json:"automatic_sync"`
		Secrets       []requiredSecret `json:"secrets"`
		PullSecret    string           `json:"pull_secret,omitempty"`
	}{Schema: "forge.deploy.gitops/v1", Repository: d.Target.Release.Repo, Branch: d.Target.Release.Branch, Path: d.Target.Release.Path, Controller: controller, Context: d.Target.Context, Namespace: namespace(d), Secrets: secrets, PullSecret: d.Target.Build.Registry.PullSecret}
	if handoff.Branch == "" {
		handoff.Branch = "main"
	}

	raw, err := json.MarshalIndent(handoff, "", "  ")
	if err != nil {
		return err
	}

	b.Add("gitops-handoff.json", append(raw, '\n'))

	guide := "# GitOps handoff\n\nCommit the complete Kustomize bundle at the repository, branch and path in gitops-handoff.json. Configure your existing controller for the intended cluster and namespace, then review its diff before a manual sync. Forge does not write to that repository or change controller policies.\n\nProvision external resources and existing Secrets first. The handoff lists each Secret name and required key; their values are never generated into Git. Supply the named image pull Secret before syncing private images.\n\nKeep pruning and automatic sync disabled for this handoff. Service selection does not authorize removing other workloads, volumes or namespaces. Confirm those policies on your controller because Forge cannot enforce them through an export.\n\nMigration and one-off job ordering is unqualified for this controller handoff. Use direct deployment for those operations. CLI and page apply reject GitOps profiles; controller health is not recorded as a Forge release.\n"
	b.Add("GITOPS.md", []byte(guide))

	return nil
}
