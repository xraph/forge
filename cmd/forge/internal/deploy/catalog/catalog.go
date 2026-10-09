// Package catalog holds the descriptors that say how a Forge extension
// declares its backends, and the recipes that can host those backends.
package catalog

import (
	"embed"
	"fmt"
	"io/fs"
	"slices"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/xraph/forge/cmd/forge/internal/deploy/model"
)

//go:embed extensions/*.yaml recipes/*.yaml
var embedded embed.FS

const descriptorSchema = 1

type Descriptor struct {
	Schema     int                           `yaml:"schema"`
	Extension  string                        `yaml:"extension"`
	ConfigKey  string                        `yaml:"config_key"`
	Instances  *Instances                    `yaml:"instances,omitempty"`
	Kinds      map[string]model.ResourceType `yaml:"kinds,omitempty"`
	Bind       map[string]string             `yaml:"bind,omitempty"`
	Requires   []Requirement                 `yaml:"requires,omitempty"`
	Optional   []OptionalRequirement         `yaml:"optional,omitempty"`
	Migrations *Migrations                   `yaml:"migrations,omitempty"`
	Source     string                        `yaml:"-"` // "embedded", module path, or project file
}

type Instances struct {
	Path        string   `yaml:"path"`
	Name        string   `yaml:"name"`
	DefaultFrom []string `yaml:"default_from,omitempty"`
}

type Requirement struct {
	Extension string `yaml:"extension"`
	Via       string `yaml:"via"`
}

type OptionalRequirement struct {
	Kind model.ResourceType `yaml:"kind"`
	When *Condition         `yaml:"when,omitempty"`
	Bind map[string]string  `yaml:"bind,omitempty"`
}

type Condition struct {
	Field  string `yaml:"field"`
	Equals string `yaml:"equals"`
}

type Migrations struct {
	Owner bool `yaml:"owner"`
}

type Recipe struct {
	ID          string             `yaml:"id"`
	Type        model.ResourceType `yaml:"type"`
	Version     string             `yaml:"version"`
	Image       string             `yaml:"image"`
	Features    []string           `yaml:"features,omitempty"`
	Env         map[string]string  `yaml:"env,omitempty"` // values may reference ${SECRET}
	Port        int                `yaml:"port"`
	Volume      string             `yaml:"volume,omitempty"` // container path to persist
	Healthcheck []string           `yaml:"healthcheck"`
	Default     bool               `yaml:"default,omitempty"`
	Command     []string           `yaml:"command,omitempty"`
	InitImage   string             `yaml:"init_image,omitempty"`
	InitEnv     map[string]string  `yaml:"init_env,omitempty"`
	Init        []string           `yaml:"init,omitempty"` // one-shot after healthy, e.g. bucket creation
	DSN         string             `yaml:"dsn"`            // template: "postgres://${USER}:${PASSWORD}@{host}:{port}/{database}"
	Source      string             `yaml:"-"`
}

type Catalog struct {
	Descriptors map[string]Descriptor // by extension name
	Recipes     map[string]Recipe     // by id
}

type Module struct {
	Path string // module path
	Dir  string // on-disk directory from go list -m -json
}

// Embedded returns the catalog shipped in the CLI.
func Embedded() *Catalog {
	c := &Catalog{Descriptors: map[string]Descriptor{}, Recipes: map[string]Recipe{}}
	_ = fs.WalkDir(embedded, "extensions", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, _ := embedded.ReadFile(p)

		var desc Descriptor
		if err := yaml.Unmarshal(data, &desc); err != nil {
			panic(fmt.Sprintf("embedded descriptor %s: %v", p, err))
		}

		desc.Source = "embedded"
		c.Descriptors[desc.Extension] = desc

		return nil
	})
	_ = fs.WalkDir(embedded, "recipes", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}

		data, _ := embedded.ReadFile(p)

		var r Recipe
		if err := yaml.Unmarshal(data, &r); err != nil {
			panic(fmt.Sprintf("embedded recipe %s: %v", p, err))
		}

		r.Source = "embedded"
		c.Recipes[r.ID] = r

		return nil
	})

	return c
}

// DescriptorFor returns the descriptor for an extension name.
func (c *Catalog) DescriptorFor(extension string) (Descriptor, bool) {
	d, ok := c.Descriptors[extension]

	return d, ok
}

// RecipeFor picks the recipe for a type that provides every feature asked
// for. version "" accepts any. Ties resolve to the recipe with the fewest
// extra features, then by id.
func (c *Catalog) RecipeFor(t model.ResourceType, version string, features []string) (Recipe, bool) {
	var candidates []Recipe

	for _, r := range c.Recipes {
		if r.Type != t {
			continue
		}

		if version != "" && r.Version != version {
			continue
		}

		if !providesAll(r.Features, features) {
			continue
		}

		candidates = append(candidates, r)
	}

	if len(candidates) == 0 {
		return Recipe{}, false
	}

	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Default != candidates[j].Default {
			return candidates[i].Default
		}

		if len(candidates[i].Features) != len(candidates[j].Features) {
			return len(candidates[i].Features) < len(candidates[j].Features)
		}

		return candidates[i].ID < candidates[j].ID
	})

	return candidates[0], true
}

func providesAll(have, want []string) bool {
	for _, w := range want {
		found := slices.Contains(have, w)

		if !found {
			return false
		}
	}

	return true
}

// KindOf maps a driver value or DSN to a resource type through the
// descriptor's kinds table. For a DSN the scheme before ':' is used. ok is
// false when the value is unknown; an empty type with ok true means embedded.
func (d Descriptor) KindOf(value string) (model.ResourceType, bool) {
	key := value
	if i := strings.Index(value, "://"); i > 0 {
		key = value[:i]
	} else if i := strings.Index(value, ":"); i > 0 {
		key = value[:i]
	}

	t, ok := d.Kinds[strings.ToLower(key)]

	return t, ok
}
