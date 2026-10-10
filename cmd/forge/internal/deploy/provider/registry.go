package provider

import (
	"errors"
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"reflect"
	"sort"
	"strings"
)

type Factory func(execx.Runner, string) Provider
type Registry struct{ providers map[string]Provider }

func NewRegistry(runner execx.Runner, root string, factories ...Factory) *Registry {
	r := &Registry{providers: map[string]Provider{}}

	for _, f := range factories {
		p := f(runner, root)
		r.providers[p.Name()] = p
	}

	return r
}
func (r *Registry) Get(name string) (Provider, bool) {
	p, ok := r.providers[name]

	return p, ok
}
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.providers))
	for n := range r.providers {
		out = append(out, n)
	}

	sort.Strings(out)

	return out
}

// Register adds a trusted adapter without replacing an existing provider.
func (r *Registry) Register(p Provider) error {
	if p == nil || (reflect.ValueOf(p).Kind() == reflect.Pointer && reflect.ValueOf(p).IsNil()) || strings.TrimSpace(p.Name()) == "" {
		return errors.New("provider factory returned an invalid adapter")
	}

	if _, ok := r.providers[p.Name()]; ok {
		return errors.New("provider name is already registered")
	}

	r.providers[p.Name()] = p

	return nil
}
