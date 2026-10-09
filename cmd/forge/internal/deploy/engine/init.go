package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/xraph/forge/cmd/forge/internal/deploy/discover"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
	"github.com/xraph/forge/cmd/forge/internal/deploy/spec"
)

// Init writes the suggested deploy block. answers maps a decision path to
// its chosen option. With write false it returns the bytes it would write.
func (e *Engine) Init(ctx context.Context, answers map[string]string, write bool) (*InspectResult, map[string][]byte, error) {
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

	if res.Doc.Deploy != nil {
		return res, nil, output.Fail(output.ExitInvalidInput, "a deploy section already exists",
			output.Diagnostic{Code: "DEPLOY_SPEC_KEY_CONFLICT", Severity: output.SeverityError, File: res.Doc.Path, Field: "deploy",
				Message: "deploy: already present", Fix: "edit it directly, or remove it and run init again"})
	}

	block, open := discover.Block(res.Discovery.Suggestions, spec.Defaults{})

	var unanswered output.Diagnostics

	for _, d := range open {
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

	files, err := res.Doc.Patch(ops)
	if err != nil {
		return res, nil, err
	}

	if write {
		for path, data := range files {
			hash := res.Doc.Hash
			if path != res.Doc.Path {
				hash = res.Doc.Splits[path].Hash
			}

			if err := spec.Write(path, hash, data); err != nil {
				return res, nil, err
			}
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
