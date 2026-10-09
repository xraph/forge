package engine

import (
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

// Init writes the suggested deploy block. answers maps a decision path to
// its chosen option. With write false it returns the bytes it would write.
func (e *Engine) Init(ctx context.Context, answers map[string]string, write bool) (*InspectResult, map[string][]byte, error) {
	return e.InitWithOptions(ctx, answers, InitOptions{Write: write})
}

type InitOptions struct{ Force, Write bool }

func (e *Engine) InitWithOptions(ctx context.Context, answers map[string]string, opts InitOptions) (*InspectResult, map[string][]byte, error) {
	res, err := e.load(ctx)
	if err != nil {
		return nil, nil, err
	}

	if res.Diagnostics.HasErrors() {
		return res, nil, output.Fail(output.ExitInvalidInput, "config discovery failed", res.Diagnostics...)
	}

	if res.Doc.IsV1 {
		return res, nil, output.Fail(output.ExitInvalidInput, "legacy deployment exists; run forge deploy migrate")
	}

	if res.Doc.Deploy != nil && !opts.Force {
		return res, nil, output.Fail(output.ExitInvalidInput, "a deploy section already exists",
			output.Diagnostic{Code: "DEPLOY_SPEC_KEY_CONFLICT", Severity: output.SeverityError, File: res.Doc.Path, Field: "deploy",
				Message: "deploy: already present", Fix: "edit it directly, or remove it and run init again"})
	}

	block, open := discover.Block(res.Discovery.Suggestions, spec.Defaults{})

	existing := deployMap(res.Doc)
	if opts.Force {
		block = fillMissing(existing, block)
	}

	var unanswered output.Diagnostics

	for _, d := range open {
		if opts.Force {
			if _, ok := mapValue(existing, strings.TrimPrefix(d.Path, "deploy.")); ok {
				continue
			}
		}

		parts := strings.Split(d.Path, ".")
		if len(parts) > 3 && parts[1] == "resources" {
			if answers["deploy.resources."+parts[2]] == "skip" {
				continue
			}
		}

		choice, ok := answers[d.Path]
		if !ok {
			unanswered = append(unanswered, output.Diagnostic{Code: "DEPLOY_DECISION_OPEN", Severity: output.SeverityError,
				Message: d.Question, Field: d.Path, Fix: fmt.Sprintf("--answer %s=%s", d.Path, strings.Join(d.Options, "|"))})

			continue
		}

		valid := false

		for _, option := range d.Options {
			if option == choice {
				valid = true
			}
		}

		if !valid {
			return res, nil, output.Fail(output.ExitInvalidInput, "invalid answer for "+d.Path)
		}

		if d.Kind == discover.SuggestResource {
			if choice == "include" {
				parts := strings.Split(d.Path, ".")
				block["resources"].(map[string]any)[parts[2]] = d.Value
			}

			continue
		}

		applyAnswer(block, d.Path, choice)
	}

	if len(unanswered) > 0 {
		if e.mode.NonInteractive {
			return res, nil, output.Fail(output.ExitUnresolved, "decisions need answers", unanswered...)
		}
		// Interactive answers come from the plugin through answers; reaching
		// here interactively means the plugin did not ask. Fail the same way.
		return res, nil, output.Fail(output.ExitUnresolved, "decisions need answers", unanswered...)
	}

	ops := []spec.Op{{Path: "deploy", Value: block}}
	if opts.Force {
		ops = missingOps(existing, block, "deploy")
	}

	files, err := res.Doc.Patch(ops)
	if err != nil {
		return res, nil, err
	}

	raw, err := yaml.Marshal(block)
	if err != nil {
		return res, nil, err
	}

	var proposed spec.Deploy
	if err := yaml.Unmarshal(raw, &proposed); err != nil {
		return res, nil, err
	}

	check := *res.Doc
	check.Deploy = &proposed

	apps := []string{}
	for _, app := range res.Discovery.Apps {
		apps = append(apps, app.Name)
	}

	if diags := spec.Validate(&check, apps); diags.HasErrors() {
		return res, nil, output.Fail(output.ExitInvalidInput, "proposal is invalid", diags...)
	}

	if opts.Write {
		if err := e.SaveInit(ctx, res, files); err != nil {
			return res, nil, err
		}
	}

	return res, files, nil
}

// applyAnswer sets the field a decision controls.
func applyAnswer(block map[string]any, path, choice string) {
	parts := strings.Split(path, ".")[1:] // drop "deploy"

	cur := block
	for _, p := range parts[:len(parts)-1] {
		next, _ := cur[p].(map[string]any)
		if next == nil {
			next = map[string]any{}
			cur[p] = next
		}

		cur = next
	}

	leaf := parts[len(parts)-1]
	switch leaf {
	case "health":
		if choice == "heartbeat" {
			cur[leaf] = map[string]any{"heartbeat": true}
		} else {
			cur[leaf] = map[string]any{"none": true}
		}
	case "features":
		if choice == "none" {
			cur[leaf] = []string{}
		} else {
			cur[leaf] = strings.Split(choice, ",")
		}
	default:
		cur[leaf] = choice
	}
}

func mapValue(root map[string]any, path string) (any, bool) {
	var cur any = root
	for part := range strings.SplitSeq(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}

		cur, ok = m[part]
		if !ok {
			return nil, false
		}
	}

	return cur, true
}
func fillMissing(existing, proposal map[string]any) map[string]any {
	out := map[string]any{}
	maps.Copy(out, existing)

	for k, v := range proposal {
		old, ok := out[k]
		if !ok {
			out[k] = v

			continue
		}

		if oldMap, ok := old.(map[string]any); ok {
			if newMap, ok := v.(map[string]any); ok {
				out[k] = fillMissing(oldMap, newMap)
			}
		}
	}

	return out
}
func deployMap(doc *spec.Document) map[string]any {
	root := map[string]any{}
	_ = doc.Root.Decode(&root)

	out, _ := root["deploy"].(map[string]any)
	if out == nil {
		return map[string]any{}
	}

	if doc.Deploy == nil {
		return out
	}

	for path, split := range doc.Splits {
		data := map[string]any{}
		_ = split.Root.Decode(&data)
		isEnvironment := false

		for env, rel := range doc.Deploy.EnvironmentFiles {
			if filepath.Join(filepath.Dir(doc.Path), rel) == path {
				envs, _ := out["environments"].(map[string]any)
				if envs == nil {
					envs = map[string]any{}
					out["environments"] = envs
				}

				envs[env] = data
				isEnvironment = true
			}
		}

		if !isEnvironment {
			maps.Copy(out, data)
		}
	}

	return out
}

func missingOps(existing, proposal map[string]any, path string) []spec.Op {
	var ops []spec.Op

	for _, k := range slices.Sorted(maps.Keys(proposal)) {
		v := proposal[k]

		old, ok := existing[k]
		if !ok {
			ops = append(ops, spec.Op{Path: path + "." + k, Value: v})

			continue
		}

		oldMap, ok := old.(map[string]any)

		newMap, newOK := v.(map[string]any)
		if ok && newOK {
			ops = append(ops, missingOps(oldMap, newMap, path+"."+k)...)
		}
	}

	return ops
}
