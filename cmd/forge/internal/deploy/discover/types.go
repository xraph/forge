// Package discover reads application metadata and proposes deployment settings.
package discover

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/catalog"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"github.com/xraph/forge/cmd/forge/internal/deploy/output"
)

type App struct {
	Name        string   `json:"name"`
	Dir         string   `json:"dir"`
	MainPath    string   `json:"main_path"`
	Module      string   `json:"module"`
	Type        string   `json:"type"`         // from cmd/<app>/.forge.yaml app.type, "" if absent
	Port        int      `json:"port"`         // from dev.port, 0 if absent
	ConfigPaths []string `json:"config_paths"` // existing config files for this app
	Imports     []string `json:"imports"`      // module paths from go list -deps, deduplicated
}

type SuggestionKind string

const (
	SuggestService  SuggestionKind = "service"
	SuggestResource SuggestionKind = "resource"
	SuggestBinding  SuggestionKind = "binding"
	SuggestDecision SuggestionKind = "decision"
)

type Confidence string

const (
	High   Confidence = "high"   // from config
	Medium Confidence = "medium" // from per-app .forge.yaml
	Low    Confidence = "low"    // from imports only
)

type Suggestion struct {
	Kind       SuggestionKind `json:"kind"`
	Path       string         `json:"path"` // the spec path it would set, e.g. "deploy.services.api.kind"
	Value      any            `json:"value"`
	Source     string         `json:"source"` // "config/api.yaml:14" or "go.mod: github.com/xraph/grove/drivers/pgdriver"
	Confidence Confidence     `json:"confidence"`
	Question   string         `json:"question"` // set for decisions
	Options    []string       `json:"options"`  // for decisions
}

type Result struct {
	Apps        []App              `json:"apps"`
	Suggestions []Suggestion       `json:"suggestions"`
	Modules     []catalog.Module   `json:"modules"`
	Diagnostics output.Diagnostics `json:"diagnostics"`
}

type Options struct {
	// GoList runs `go list` with args in dir. nil uses execx.System().
	Modules []catalog.Module `json:"modules"`
	Runner  execx.Runner
}
