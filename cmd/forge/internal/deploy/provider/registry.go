package provider

import (
	"github.com/xraph/forge/cmd/forge/internal/deploy/execx"
	"sort"
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
