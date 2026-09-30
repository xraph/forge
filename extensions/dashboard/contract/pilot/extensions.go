package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// ExtensionsProvider lists the extensions registered with the app.
type ExtensionsProvider interface {
	ListExtensions() []ExtensionInfo
}

// extensionsListHandler is exposed to the dispatcher via RegisterQuery. An
// extension with no display name is listed under its name.
func extensionsListHandler(p ExtensionsProvider) func(ctx context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
		exts := p.ListExtensions()
		out := make([]ExtensionInfo, 0, len(exts))

		for _, e := range exts {
			if e.DisplayName == "" {
				e.DisplayName = e.Name
			}

			out = append(out, e)
		}

		return ExtensionsList{Extensions: out}, nil
	}
}
