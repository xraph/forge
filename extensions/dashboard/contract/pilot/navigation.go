package pilot

import (
	"context"
	"sort"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// NavItem is one renderable sidebar entry projected from a graph route's
// NavConfig. The shell joins href with its own basePath via NavLink so we
// emit the route as-is (no /dashboard prefix bound at server time).
type NavItem struct {
	Label    string `json:"label"`
	Href     string `json:"href"`
	Icon     string `json:"icon,omitempty"`
	Badge    string `json:"badge,omitempty"`
	Priority int    `json:"priority"`
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

func navigationHandler(reg contract.Registry) func(ctx context.Context, _ struct{}, _ contract.Principal) (NavigationResponse, error) {
	return func(_ context.Context, _ struct{}, _ contract.Principal) (NavigationResponse, error) {
		if reg == nil {
			return NavigationResponse{}, &contract.Error{Code: contract.CodeUnavailable, Message: "registry not configured"}
		}
		groupMap := map[string]*NavGroup{}
		for _, m := range reg.All() {
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
					Label:    label,
					Href:     n.Route,
					Icon:     n.Nav.Icon,
					Badge:    n.Nav.Badge,
					Priority: n.Nav.Priority,
				})
			}
		}
		groups := make([]NavGroup, 0, len(groupMap))
		for _, g := range groupMap {
			sort.SliceStable(g.Items, func(i, j int) bool {
				if g.Items[i].Priority != g.Items[j].Priority {
					return g.Items[i].Priority < g.Items[j].Priority
				}
				return g.Items[i].Label < g.Items[j].Label
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
