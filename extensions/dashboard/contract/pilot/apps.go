package pilot

import (
	"context"
	"sort"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// AppInfo is the wire shape one entry in the app switcher consumes. It's a
// projection of contract.AppInfo joined with the owning contributor's name
// so the React shell can group nav items by app without a second lookup.
//
// For root apps (Root=true), Slug is empty and Home stays unprefixed —
// the platform app owns the bare URL space (/, /health, ...). For other
// apps Home is projected to /@<slug><home>.
type AppInfo struct {
	Contributor string `json:"contributor"`
	DisplayName string `json:"displayName"`
	Slug        string `json:"slug,omitempty"`
	Root        bool   `json:"root,omitempty"`
	Icon        string `json:"icon,omitempty"`
	Priority    int    `json:"priority"`
	Home        string `json:"home,omitempty"`
}

// projectAppHome rewrites the manifest's unprefixed Home into the
// /@<slug><home> form that the merged graph routes use. Identical to
// contract.prefixAppHome (lives there for use during merge); duplicated
// here to keep the apps.list projection self-contained without
// re-exporting the helper.
func projectAppHome(home, slug string) string {
	if home == "" || slug == "" {
		return home
	}
	if len(home) >= 2 && home[0] == '/' && home[1] == '@' {
		return home
	}
	if home[0] != '/' {
		return home
	}
	prefix := "/@" + slug
	if home == "/" {
		return prefix
	}
	return prefix + home
}

// AppsListResponse is the response of the apps.list query.
type AppsListResponse struct {
	Apps []AppInfo `json:"apps"`
}

// appsListHandler walks the contract registry and returns every contributor
// that opted into the switcher (declared a contributor.app: block in its
// manifest). The list is sorted by (priority asc, displayName asc) so two
// consecutive refreshes always return the same order — there's no
// map-iteration tie window even when two apps share a priority.
func appsListHandler(reg contract.Registry) func(ctx context.Context, _ struct{}, _ contract.Principal) (AppsListResponse, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (AppsListResponse, error) {
		if reg == nil {
			return AppsListResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "registry not configured"}
		}
		all := reg.All()
		out := make([]AppInfo, 0, len(all))
		for _, m := range all {
			if m.Contributor.App == nil {
				continue
			}
			a := m.Contributor.App
			displayName := a.DisplayName
			if displayName == "" {
				displayName = m.Contributor.Name
			}
			// Project the prefixed Home so the React shell navigates to
			// /@<slug><home> — matching the hrefs the navigation handler
			// emits. ResolvedSlug returns "" for root apps, which makes
			// projectAppHome a no-op: root apps own the bare URL space.
			slug := a.ResolvedSlug(m.Contributor.Name)
			home := projectAppHome(a.Home, slug)
			out = append(out, AppInfo{
				Contributor: m.Contributor.Name,
				DisplayName: displayName,
				Icon:        a.Icon,
				Priority:    a.Priority,
				Home:        home,
				Slug:        slug,
				Root:        a.Root,
			})
		}
		sort.SliceStable(out, func(i, j int) bool {
			if out[i].Priority != out[j].Priority {
				return out[i].Priority < out[j].Priority
			}
			if out[i].DisplayName != out[j].DisplayName {
				return out[i].DisplayName < out[j].DisplayName
			}
			return out[i].Contributor < out[j].Contributor
		})
		return AppsListResponse{Apps: out}, nil
	}
}
