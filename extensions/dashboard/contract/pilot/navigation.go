package pilot

import (
	"context"
	"sort"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// NavItem is one renderable sidebar entry projected from a graph route's
// NavConfig. The shell joins href with its own basePath via NavLink so we
// emit the route as-is (no /dashboard prefix bound at server time).
//
// Contributor is the contract contributor that owns the route; the React
// shell uses it to filter the sidebar when an app switcher is in play.
type NavItem struct {
	Label       string `json:"label"`
	Href        string `json:"href"`
	Icon        string `json:"icon,omitempty"`
	Badge       string `json:"badge,omitempty"`
	Priority    int    `json:"priority"`
	Contributor string `json:"contributor"`
}

// NavGroup bundles items contributed under the same Nav.Group, sorted by
// priority. Group order follows navGroupOrder below; unknown groups go last.
type NavGroup struct {
	Group    string    `json:"group"`
	Priority int       `json:"priority"`
	Items    []NavItem `json:"items"`
}

// NavigationResponse is the wire shape of the navigation query.
type NavigationResponse struct {
	Groups []NavGroup `json:"groups"`
}

// navGroupOrder mirrors contributor/registry.go's groupOrder so the contract
// sidebar matches the legacy templ sidebar's group order. Kept in-package
// rather than imported because the contributor map is unexported.
var navGroupOrder = map[string]int{
	"Overview":       0,
	"Identity":       1,
	"Security":       2,
	"AI":             3,
	"Platform":       4,
	"Configuration":  5,
	"Operations":     6,
	"Infrastructure": 7,
	"Plugins":        8,
}

const navUnknownGroupBase = 1000

// prefixHref rewrites an unprefixed manifest route into the
// /@<slug><route> form the React shell consumes. Empty slug is a no-op;
// already-prefixed hrefs (defensive) pass through untouched; relative
// hrefs (no leading slash) also pass through so we don't corrupt
// external links that authors might one day put in a NavItem.
//
// "/" maps to "/@slug" without a trailing slash so the URL stays
// consistent with every other route under the namespace.
func prefixHref(route, slug string) string {
	if slug == "" || route == "" || route[0] != '/' {
		return route
	}
	if len(route) >= 2 && route[1] == '@' {
		return route
	}
	prefix := "/@" + slug
	if route == "/" {
		return prefix
	}
	return prefix + route
}

func navigationHandler(reg contract.Registry) func(ctx context.Context, _ struct{}, _ contract.Principal) (NavigationResponse, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (NavigationResponse, error) {
		if reg == nil {
			return NavigationResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "registry not configured"}
		}
		// Sort manifests by contributor name before iterating. reg.All()
		// returns map values whose Go iteration order is randomised per
		// process — without this, two contributors with the same nav
		// (group, priority, label) would render in non-deterministic order
		// across page refreshes. The downstream sort.SliceStable below is
		// only "stable" relative to whatever input order it receives, so the
		// input order itself has to be deterministic.
		manifests := reg.All()
		sort.SliceStable(manifests, func(i, j int) bool {
			return manifests[i].Contributor.Name < manifests[j].Contributor.Name
		})
		groupMap := map[string]*NavGroup{}
		for _, m := range manifests {
			// The slug determines the URL namespace each NavItem.Href is
			// projected into: a route declared in YAML as `/users` shows
			// up to the React shell as `/@authsome/users`. Root apps and
			// library contributors (no App block) get the empty slug and
			// their routes stay unprefixed — root apps own the bare URL
			// space (/, /health, …); library contributors typically
			// don't contribute nav, but if they do the behaviour stays
			// predictable.
			slug := m.Contributor.App.ResolvedSlug(m.Contributor.Name)
			for _, n := range m.Graph {
				if n.Nav == nil {
					continue
				}
				label := n.Title
				if label == "" {
					label = n.Route
				}
				groupName := n.Nav.Group
				if groupName == "" {
					groupName = "Other"
				}
				g, ok := groupMap[groupName]
				if !ok {
					prio, known := navGroupOrder[groupName]
					if !known {
						prio = navUnknownGroupBase + len(groupMap)
					}
					g = &NavGroup{Group: groupName, Priority: prio}
					groupMap[groupName] = g
				}
				g.Items = append(g.Items, NavItem{
					Label:       label,
					Href:        prefixHref(n.Route, slug),
					Icon:        n.Nav.Icon,
					Badge:       n.Nav.Badge,
					Priority:    n.Nav.Priority,
					Contributor: m.Contributor.Name,
				})
			}
		}
		groups := make([]NavGroup, 0, len(groupMap))
		for _, g := range groupMap {
			sort.SliceStable(g.Items, func(i, j int) bool {
				if g.Items[i].Priority != g.Items[j].Priority {
					return g.Items[i].Priority < g.Items[j].Priority
				}
				if g.Items[i].Label != g.Items[j].Label {
					return g.Items[i].Label < g.Items[j].Label
				}
				// Final tiebreaker: contributor name. Two contributors that
				// declare the same (group, priority, label) — unlikely but
				// possible — still render in the same order every refresh.
				return g.Items[i].Contributor < g.Items[j].Contributor
			})
			groups = append(groups, *g)
		}
		sort.SliceStable(groups, func(i, j int) bool {
			if groups[i].Priority != groups[j].Priority {
				return groups[i].Priority < groups[j].Priority
			}
			return groups[i].Group < groups[j].Group
		})
		return NavigationResponse{Groups: groups}, nil
	}
}
