package gateway

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// RouteManager manages the dynamic route table with thread-safe operations.
type RouteManager struct {
	// routeTable is swapped atomically for lock-free reads
	routeTable atomic.Pointer[routeTable]

	// mu protects writes (add/remove/update)
	mu sync.Mutex

	// Event listeners
	listenersMu sync.RWMutex
	listeners   []func(RouteEvent)
}

type routeTable struct {
	routes  []*Route            // sorted by priority descending
	byID    map[string]*Route   // quick lookup by ID
	byPath  map[string][]*Route // index by path for conflict detection
	version int64
}

func newRouteTable() *routeTable {
	return &routeTable{
		routes:  make([]*Route, 0),
		byID:    make(map[string]*Route),
		byPath:  make(map[string][]*Route),
		version: 0,
	}
}

// clone creates a deep copy of the route table for atomic swap.
func (rt *routeTable) clone() *routeTable {
	newRT := &routeTable{
		routes:  make([]*Route, len(rt.routes)),
		byID:    make(map[string]*Route, len(rt.byID)),
		byPath:  make(map[string][]*Route),
		version: rt.version + 1,
	}

	copy(newRT.routes, rt.routes)

	for k, v := range rt.byID {
		newRT.byID[k] = v
	}

	for k, v := range rt.byPath {
		newSlice := make([]*Route, len(v))
		copy(newSlice, v)
		newRT.byPath[k] = newSlice
	}

	return newRT
}

// NewRouteManager creates a new route manager.
func NewRouteManager() *RouteManager {
	rm := &RouteManager{}
	rm.routeTable.Store(newRouteTable())

	return rm
}

// OnRouteChange registers a listener for route events.
func (rm *RouteManager) OnRouteChange(fn func(RouteEvent)) {
	rm.listenersMu.Lock()
	defer rm.listenersMu.Unlock()

	rm.listeners = append(rm.listeners, fn)
}

// AddRoute adds a new route to the table.
func (rm *RouteManager) AddRoute(route *Route) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	current := rm.routeTable.Load()

	// Check for duplicate ID
	if _, exists := current.byID[route.ID]; exists {
		return fmt.Errorf("route with ID %q already exists", route.ID)
	}

	// Assign ID if empty
	if route.ID == "" {
		route.ID = uuid.New().String()
	}

	now := time.Now()
	route.CreatedAt = now
	route.UpdatedAt = now

	// Clone and add
	newTable := current.clone()
	newTable.routes = append(newTable.routes, route)
	newTable.byID[route.ID] = route
	newTable.byPath[route.Path] = append(newTable.byPath[route.Path], route)

	// Sort by priority (descending)
	sort.Slice(newTable.routes, func(i, j int) bool {
		if newTable.routes[i].Priority != newTable.routes[j].Priority {
			return newTable.routes[i].Priority > newTable.routes[j].Priority
		}
		// Manual routes take precedence over auto-discovered
		return sourceWeight(newTable.routes[i].Source) > sourceWeight(newTable.routes[j].Source)
	})

	// Atomic swap
	rm.routeTable.Store(newTable)

	// Emit event
	rm.emit(RouteEvent{
		Type:      RouteEventAdded,
		Route:     route,
		Timestamp: now,
	})

	return nil
}

// RemoveRoute removes a route by ID.
func (rm *RouteManager) RemoveRoute(id string) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	current := rm.routeTable.Load()

	route, exists := current.byID[id]
	if !exists {
		return fmt.Errorf("route %q not found", id)
	}

	newTable := current.clone()
	delete(newTable.byID, id)

	// Remove from routes slice
	for i, r := range newTable.routes {
		if r.ID == id {
			newTable.routes = append(newTable.routes[:i], newTable.routes[i+1:]...)

			break
		}
	}

	// Remove from path index
	if pathRoutes, ok := newTable.byPath[route.Path]; ok {
		for i, r := range pathRoutes {
			if r.ID == id {
				newTable.byPath[route.Path] = append(pathRoutes[:i], pathRoutes[i+1:]...)

				break
			}
		}

		if len(newTable.byPath[route.Path]) == 0 {
			delete(newTable.byPath, route.Path)
		}
	}

	rm.routeTable.Store(newTable)

	rm.emit(RouteEvent{
		Type:      RouteEventRemoved,
		Route:     route,
		Timestamp: time.Now(),
	})

	return nil
}

// UpdateRoute updates an existing route.
func (rm *RouteManager) UpdateRoute(route *Route) error {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	current := rm.routeTable.Load()

	existing, exists := current.byID[route.ID]
	if !exists {
		return fmt.Errorf("route %q not found", route.ID)
	}

	route.CreatedAt = existing.CreatedAt
	route.UpdatedAt = time.Now()
	route.Version = existing.Version + 1

	newTable := current.clone()

	// Replace in routes slice
	for i, r := range newTable.routes {
		if r.ID == route.ID {
			newTable.routes[i] = route

			break
		}
	}

	// Update path index if path changed
	if existing.Path != route.Path {
		// Remove from old path
		if pathRoutes, ok := newTable.byPath[existing.Path]; ok {
			for i, r := range pathRoutes {
				if r.ID == route.ID {
					newTable.byPath[existing.Path] = append(pathRoutes[:i], pathRoutes[i+1:]...)

					break
				}
			}
		}

		// Add to new path
		newTable.byPath[route.Path] = append(newTable.byPath[route.Path], route)
	} else {
		// Update in-place in path index
		if pathRoutes, ok := newTable.byPath[route.Path]; ok {
			for i, r := range pathRoutes {
				if r.ID == route.ID {
					pathRoutes[i] = route

					break
				}
			}
		}
	}

	newTable.byID[route.ID] = route

	// Re-sort
	sort.Slice(newTable.routes, func(i, j int) bool {
		if newTable.routes[i].Priority != newTable.routes[j].Priority {
			return newTable.routes[i].Priority > newTable.routes[j].Priority
		}

		return sourceWeight(newTable.routes[i].Source) > sourceWeight(newTable.routes[j].Source)
	})

	rm.routeTable.Store(newTable)

	rm.emit(RouteEvent{
		Type:      RouteEventUpdated,
		Route:     route,
		Timestamp: time.Now(),
	})

	return nil
}

// GetRoute returns a route by ID.
func (rm *RouteManager) GetRoute(id string) (*Route, bool) {
	table := rm.routeTable.Load()
	route, ok := table.byID[id]

	return route, ok
}

// ListRoutes returns all routes.
func (rm *RouteManager) ListRoutes() []*Route {
	table := rm.routeTable.Load()
	result := make([]*Route, len(table.routes))
	copy(result, table.routes)

	return result
}

// MatchRoute finds the best matching route for a request path and method.
func (rm *RouteManager) MatchRoute(path, method string) *Route {
	table := rm.routeTable.Load()

	// Routes are sorted by priority, first match wins
	for _, route := range table.routes {
		if !route.Enabled {
			continue
		}

		if matchPath(route.Path, path) {
			if len(route.Methods) == 0 || containsMethod(route.Methods, method) {
				return route
			}
		}
	}

	return nil
}

// RouteCount returns the number of routes.
func (rm *RouteManager) RouteCount() int {
	table := rm.routeTable.Load()

	return len(table.routes)
}

// RemoveBySource removes all routes from a specific source.
func (rm *RouteManager) RemoveBySource(source RouteSource) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	current := rm.routeTable.Load()
	newTable := newRouteTable()
	newTable.version = current.version + 1

	var removed []*Route

	for _, r := range current.routes {
		if r.Source == source {
			removed = append(removed, r)

			continue
		}

		newTable.routes = append(newTable.routes, r)
		newTable.byID[r.ID] = r
		newTable.byPath[r.Path] = append(newTable.byPath[r.Path], r)
	}

	rm.routeTable.Store(newTable)

	for _, r := range removed {
		rm.emit(RouteEvent{
			Type:      RouteEventRemoved,
			Route:     r,
			Timestamp: time.Now(),
		})
	}
}

// RemoveByServiceName removes all routes for a service.
func (rm *RouteManager) RemoveByServiceName(serviceName string) {
	rm.mu.Lock()
	defer rm.mu.Unlock()

	current := rm.routeTable.Load()
	newTable := newRouteTable()
	newTable.version = current.version + 1

	var removed []*Route

	for _, r := range current.routes {
		if r.ServiceName == serviceName {
			removed = append(removed, r)

			continue
		}

		newTable.routes = append(newTable.routes, r)
		newTable.byID[r.ID] = r
		newTable.byPath[r.Path] = append(newTable.byPath[r.Path], r)
	}

	rm.routeTable.Store(newTable)

	for _, r := range removed {
		rm.emit(RouteEvent{
			Type:      RouteEventRemoved,
			Route:     r,
			Timestamp: time.Now(),
		})
	}
}

func (rm *RouteManager) emit(event RouteEvent) {
	rm.listenersMu.RLock()
	listeners := make([]func(RouteEvent), len(rm.listeners))
	copy(listeners, rm.listeners)
	rm.listenersMu.RUnlock()

	for _, fn := range listeners {
		go fn(event)
	}
}

// matchPath checks if a request path matches a route pattern.
// Supports exact match, prefix match with /*, and path parameters with :param.
func matchPath(pattern, path string) bool {
	// Exact match
	if pattern == path {
		return true
	}

	// Wildcard suffix match
	if strings.HasSuffix(pattern, "/*") {
		prefix := strings.TrimSuffix(pattern, "/*")
		if strings.HasPrefix(path, prefix+"/") || path == prefix {
			return true
		}
	}

	// Wildcard prefix match (catch-all)
	if pattern == "/*" || pattern == "/" {
		return true
	}

	// Path parameter matching
	patternParts := strings.Split(strings.Trim(pattern, "/"), "/")
	pathParts := strings.Split(strings.Trim(path, "/"), "/")

	if len(patternParts) != len(pathParts) {
		// Check if last pattern part is wildcard
		if len(patternParts) > 0 && patternParts[len(patternParts)-1] == "*" {
			if len(pathParts) >= len(patternParts)-1 {
				patternParts = patternParts[:len(patternParts)-1]
				pathParts = pathParts[:len(patternParts)]
			} else {
				return false
			}
		} else {
			return false
		}
	}

	for i, pp := range patternParts {
		if strings.HasPrefix(pp, ":") {
			continue // Parameter matches anything
		}

		if pp == "*" {
			continue // Wildcard matches anything
		}

		if pp != pathParts[i] {
			return false
		}
	}

	return true
}

func containsMethod(methods []string, method string) bool {
	method = strings.ToUpper(method)

	for _, m := range methods {
		if strings.ToUpper(m) == method {
			return true
		}
	}

	return false
}

func sourceWeight(source RouteSource) int {
	switch source {
	case SourceManual:
		return 3
	case SourceFARP:
		return 2
	case SourceDiscovery:
		return 1
	default:
		return 0
	}
}
