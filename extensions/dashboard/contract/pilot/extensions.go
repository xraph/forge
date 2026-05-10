package pilot

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// extensionsListHandler is exposed to the dispatcher via RegisterQuery.
// Mirrors the existing /api/extensions JSON shape so consumers can compare directly.
func extensionsListHandler(reg *contributor.ContributorRegistry) func(ctx context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (ExtensionsList, error) {
		names := reg.ContributorNames()
		out := make([]ExtensionInfo, 0, len(names))
		for _, name := range names {
			m, ok := reg.GetManifest(name)
			if !ok {
				continue
			}
			displayName := m.DisplayName
			if displayName == "" {
				displayName = name
			}
			out = append(out, ExtensionInfo{
				Name:        m.Name,
				DisplayName: displayName,
				Version:     m.Version,
				Icon:        m.Icon,
				Layout:      m.Layout,
				PageCount:   len(m.Nav),
				WidgetCount: len(m.Widgets),
			})
		}
		return ExtensionsList{Extensions: out}, nil
	}
}
