package pages

import (
	"context"
	"fmt"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// ExtensionInfo holds display data for a single extension on the extensions listing page.
type ExtensionInfo struct {
	Name        string
	DisplayName string
	Icon        string
	Version     string
	Layout      string
	EntryPath   string
	WidgetCount int
	PageCount   int
	IsCore      bool
}

// ExtensionsPage collects extension info from the registry and returns the page component.
func ExtensionsPage(ctx context.Context, registry *contributor.ContributorRegistry, basePath string) (templ.Component, error) {
	names := registry.ContributorNames()
	extensions := make([]ExtensionInfo, 0, len(names))

	for _, name := range names {
		m, ok := registry.GetManifest(name)
		if !ok {
			continue
		}

		displayName := m.DisplayName
		if displayName == "" {
			displayName = name
		}

		icon := m.Icon
		if icon == "" {
			icon = "puzzle"
		}

		layout := m.Layout
		if layout == "" {
			layout = "dashboard"
		}

		// Determine entry path for the extension
		entryPath := fmt.Sprintf("%s/ext/%s/pages", basePath, name)
		if m.Root {
			entryPath = basePath
			if len(m.Nav) > 0 {
				entryPath = basePath + m.Nav[0].Path
			}
		} else if len(m.Nav) > 0 {
			entryPath = fmt.Sprintf("%s/ext/%s/pages%s", basePath, name, m.Nav[0].Path)
		}

		extensions = append(extensions, ExtensionInfo{
			Name:        name,
			DisplayName: displayName,
			Icon:        icon,
			Version:     m.Version,
			Layout:      layout,
			EntryPath:   entryPath,
			WidgetCount: len(m.Widgets),
			PageCount:   len(m.Nav),
			IsCore:      name == "core",
		})
	}

	return ExtensionsPageContent(extensions, basePath), nil
}

// extensionLayoutLabel returns a human-readable label for the extension layout type.
func extensionLayoutLabel(layout string) string {
	switch layout {
	case "dashboard":
		return "Dashboard"
	case "extension":
		return "Standalone"
	case "settings":
		return "Settings"
	case "full":
		return "Full Page"
	default:
		return layout
	}
}
