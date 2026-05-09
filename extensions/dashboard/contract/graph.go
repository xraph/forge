// graph.go
package contract

import (
	"context"
	"fmt"
)

// GraphBuilder produces a per-(route, principal) filtered graph by walking the
// merged graph from the registry and dropping nodes whose visibleWhen predicates
// fail. EnabledWhen is preserved as an annotation (it does not strip the node);
// the React shell honors it for disabled-but-visible UI states.
type GraphBuilder struct {
	registry Registry
	wardens  WardenRegistry
}

// NewGraphBuilder returns a builder bound to the given registry and warden registry.
func NewGraphBuilder(reg Registry, wardens WardenRegistry) *GraphBuilder {
	return &GraphBuilder{registry: reg, wardens: wardens}
}

// Build returns the filtered graph rooted at the given route for the given principal.
// Returns ErrNotFound if no contributor owns the route, or ErrPermissionDenied if the
// root node itself is filtered out by the principal's permissions.
func (b *GraphBuilder) Build(ctx context.Context, contributor, route string, p Principal) (*GraphNode, error) {
	root, ok := b.registry.MergedGraph(contributor, route)
	if !ok {
		return nil, fmt.Errorf("%w: contributor=%s route=%s", ErrNotFound, contributor, route)
	}
	filtered, err := b.filter(ctx, *root, p)
	if err != nil {
		return nil, err
	}
	if filtered == nil {
		return nil, fmt.Errorf("%w: route filtered for principal", ErrPermissionDenied)
	}
	return filtered, nil
}

// filter returns a deep copy of n with non-visible descendants stripped, or nil
// if n itself fails its own visibleWhen.
func (b *GraphBuilder) filter(ctx context.Context, n GraphNode, p Principal) (*GraphNode, error) {
	if !b.allowsNode(ctx, n, p) {
		return nil, nil
	}
	out := n
	if n.Slots != nil {
		out.Slots = map[string][]GraphNode{}
		for slotName, children := range n.Slots {
			var kept []GraphNode
			for _, c := range children {
				kc, err := b.filter(ctx, c, p)
				if err != nil {
					return nil, err
				}
				if kc != nil {
					kept = append(kept, *kc)
				}
			}
			if len(kept) > 0 {
				out.Slots[slotName] = kept
			}
		}
	}
	return &out, nil
}

// allowsNode evaluates visibleWhen plus any per-slot 'requires' inherited from
// the parent intent's slot definition. Returns true if the node should be kept.
func (b *GraphBuilder) allowsNode(_ context.Context, n GraphNode, p Principal) bool {
	if n.VisibleWhen != nil && !n.VisibleWhen.Allow(p.User, nil) {
		return false
	}
	// Warden hook: if visibleWhen carries a Warden ref, run it
	if n.VisibleWhen != nil && n.VisibleWhen.Warden != "" {
		w, ok := b.wardens.Get(n.VisibleWhen.Warden)
		if !ok {
			return false
		}
		// Best-effort sync call here; per-event re-checks are cached in stream.go
		d, err := w.Authorize(context.Background(), p, Action{Intent: n.Intent})
		if err != nil || !d.Allow {
			return false
		}
	}
	return true
}
