package contributor

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// NavGroup is a merged sidebar section built from contributor manifests.
type NavGroup struct {
	Name     string // "Platform", "Identity", etc.
	Icon     string
	Items    []ResolvedNav // merged + sorted by priority
	Priority int
}

// ResolvedNav is a NavItem enriched with its owning contributor name and resolved path.
type ResolvedNav struct {
	NavItem

	Contributor string        // contributor name (e.g., "authsome", "nexus")
	FullPath    string        // resolved path (e.g., "/dashboard/ext/authsome/pages/users")
	Children    []ResolvedNav // resolved child nav items (from NavItem.Children)
}

// ResolvedWidget is a WidgetDescriptor enriched with its owning contributor name.
type ResolvedWidget struct {
	WidgetDescriptor

	Contributor string
}

// ResolvedSetting is a SettingsDescriptor enriched with its owning contributor name.
type ResolvedSetting struct {
	SettingsDescriptor

	Contributor string
}

// predefined group ordering for stable sidebar layout.
var groupOrder = map[string]int{
	"Overview":       0,
	"Identity":       1,
	"AI":             2,
	"Platform":       3,
	"Operations":     4,
	"Infrastructure": 5,
}

// ContributorRegistry manages all registered contributors and merges their manifests.
// It is thread-safe for concurrent reads and writes.
type ContributorRegistry struct {
	mu          sync.RWMutex
	local       map[string]LocalContributor
	remote      map[string]DashboardContributor
	ssr         map[string]*SSRContributor // SSR contributors tracked for lifecycle management
	manifests   map[string]*Manifest
	navGroups   []NavGroup
	allWidgets  []ResolvedWidget
	allSettings []ResolvedSetting
	dirty       bool // true when nav/widgets/settings need rebuild
}

// NewContributorRegistry creates a new empty contributor registry.
func NewContributorRegistry() *ContributorRegistry {
	return &ContributorRegistry{
		local:     make(map[string]LocalContributor),
		remote:    make(map[string]DashboardContributor),
		ssr:       make(map[string]*SSRContributor),
		manifests: make(map[string]*Manifest),
	}
}

// RegisterLocal registers a local (in-process) contributor.
// This includes EmbeddedContributor (which implements LocalContributor).
func (r *ContributorRegistry) RegisterLocal(c LocalContributor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m := c.Manifest()
	if m == nil {
		return errors.New("contributor has nil manifest")
	}

	if _, exists := r.local[m.Name]; exists {
		return fmt.Errorf("%w: %s", ErrContributorExists, m.Name)
	}

	if _, exists := r.remote[m.Name]; exists {
		return fmt.Errorf("%w: %s (registered as remote)", ErrContributorExists, m.Name)
	}

	if _, exists := r.ssr[m.Name]; exists {
		return fmt.Errorf("%w: %s (registered as SSR)", ErrContributorExists, m.Name)
	}

	r.local[m.Name] = c
	r.manifests[m.Name] = m
	r.dirty = true

	return nil
}

// RegisterSSR registers an SSR contributor (Node.js sidecar).
// SSR contributors are tracked separately for lifecycle management (Start/Stop)
// and are also registered as local contributors for rendering.
func (r *ContributorRegistry) RegisterSSR(c *SSRContributor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m := c.Manifest()
	if m == nil {
		return errors.New("contributor has nil manifest")
	}

	if _, exists := r.local[m.Name]; exists {
		return fmt.Errorf("%w: %s (registered as local)", ErrContributorExists, m.Name)
	}

	if _, exists := r.remote[m.Name]; exists {
		return fmt.Errorf("%w: %s (registered as remote)", ErrContributorExists, m.Name)
	}

	if _, exists := r.ssr[m.Name]; exists {
		return fmt.Errorf("%w: %s", ErrContributorExists, m.Name)
	}

	r.ssr[m.Name] = c
	r.manifests[m.Name] = m
	r.dirty = true

	return nil
}

// RegisterRemote registers a remote (over-the-network) contributor.
func (r *ContributorRegistry) RegisterRemote(c DashboardContributor) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	m := c.Manifest()
	if m == nil {
		return errors.New("contributor has nil manifest")
	}

	if _, exists := r.local[m.Name]; exists {
		return fmt.Errorf("%w: %s (registered as local)", ErrContributorExists, m.Name)
	}

	if _, exists := r.remote[m.Name]; exists {
		return fmt.Errorf("%w: %s", ErrContributorExists, m.Name)
	}

	if _, exists := r.ssr[m.Name]; exists {
		return fmt.Errorf("%w: %s (registered as SSR)", ErrContributorExists, m.Name)
	}

	r.remote[m.Name] = c
	r.manifests[m.Name] = m
	r.dirty = true

	return nil
}

// Unregister removes a contributor by name (local or remote).
func (r *ContributorRegistry) Unregister(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	_, localOK := r.local[name]
	_, remoteOK := r.remote[name]
	_, ssrOK := r.ssr[name]

	if !localOK && !remoteOK && !ssrOK {
		return fmt.Errorf("%w: %s", ErrContributorNotFound, name)
	}

	delete(r.local, name)
	delete(r.remote, name)
	delete(r.ssr, name)
	delete(r.manifests, name)
	r.dirty = true

	return nil
}

// RebuildNavigation merges all manifests into NavGroups, sorted by priority.
// This must be called explicitly after registrations are complete, or it will
// be called lazily by GetNavGroups if the registry is dirty.
func (r *ContributorRegistry) RebuildNavigation() {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.rebuildLocked()
}

// rebuildLocked rebuilds navigation, widgets, and settings. Caller must hold write lock.
func (r *ContributorRegistry) rebuildLocked() {
	groupMap := make(map[string]*NavGroup)

	var (
		widgets  []ResolvedWidget
		settings []ResolvedSetting
	)

	for name, m := range r.manifests {
		// Merge navigation
		for _, nav := range m.Nav {
			groupName := nav.Group
			if groupName == "" {
				groupName = "Platform"
			}

			group, ok := groupMap[groupName]
			if !ok {
				priority, hasPredefined := groupOrder[groupName]
				if !hasPredefined {
					priority = 100 // custom groups appear last
				}

				group = &NavGroup{
					Name:     groupName,
					Priority: priority,
				}
				groupMap[groupName] = group
			}

			fullPath := fmt.Sprintf("/dashboard/ext/%s/pages%s", name, nav.Path)
			if m.Root {
				fullPath = "/dashboard" + nav.Path
			}

			resolved := ResolvedNav{
				NavItem:     nav,
				Contributor: name,
				FullPath:    fullPath,
			}

			// Resolve children nav items
			if len(nav.Children) > 0 {
				resolved.Children = make([]ResolvedNav, 0, len(nav.Children))
				for _, child := range nav.Children {
					childFullPath := fullPath + child.Path
					resolved.Children = append(resolved.Children, ResolvedNav{
						NavItem:     child,
						Contributor: name,
						FullPath:    childFullPath,
					})
				}
			}

			group.Items = append(group.Items, resolved)
		}

		// Merge widgets
		for _, w := range m.Widgets {
			widgets = append(widgets, ResolvedWidget{
				WidgetDescriptor: w,
				Contributor:      name,
			})
		}

		// Merge settings
		for _, s := range m.Settings {
			settings = append(settings, ResolvedSetting{
				SettingsDescriptor: s,
				Contributor:        name,
			})
		}
	}

	// Sort items within each group by priority
	for _, group := range groupMap {
		sort.Slice(group.Items, func(i, j int) bool {
			return group.Items[i].Priority < group.Items[j].Priority
		})
	}

	// Convert map to sorted slice of groups
	groups := make([]NavGroup, 0, len(groupMap))
	for _, group := range groupMap {
		groups = append(groups, *group)
	}

	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Priority < groups[j].Priority
	})

	// Sort widgets by priority
	sort.Slice(widgets, func(i, j int) bool {
		return widgets[i].Priority < widgets[j].Priority
	})

	// Sort settings by priority
	sort.Slice(settings, func(i, j int) bool {
		return settings[i].Priority < settings[j].Priority
	})

	r.navGroups = groups
	r.allWidgets = widgets
	r.allSettings = settings
	r.dirty = false
}

// GetNavGroups returns the merged navigation groups. Rebuilds if dirty.
func (r *ContributorRegistry) GetNavGroups() []NavGroup {
	r.mu.RLock()

	if !r.dirty {
		defer r.mu.RUnlock()

		result := make([]NavGroup, len(r.navGroups))
		copy(result, r.navGroups)

		return result
	}

	r.mu.RUnlock()

	// Need write lock to rebuild
	r.mu.Lock()
	defer r.mu.Unlock()

	// Double-check after acquiring write lock
	if r.dirty {
		r.rebuildLocked()
	}

	result := make([]NavGroup, len(r.navGroups))
	copy(result, r.navGroups)

	return result
}

// GetAllWidgets returns all resolved widgets from all contributors. Rebuilds if dirty.
func (r *ContributorRegistry) GetAllWidgets() []ResolvedWidget {
	r.mu.RLock()

	if !r.dirty {
		defer r.mu.RUnlock()

		result := make([]ResolvedWidget, len(r.allWidgets))
		copy(result, r.allWidgets)

		return result
	}

	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.dirty {
		r.rebuildLocked()
	}

	result := make([]ResolvedWidget, len(r.allWidgets))
	copy(result, r.allWidgets)

	return result
}

// GetAllSettings returns all resolved settings from all contributors. Rebuilds if dirty.
func (r *ContributorRegistry) GetAllSettings() []ResolvedSetting {
	r.mu.RLock()

	if !r.dirty {
		defer r.mu.RUnlock()

		result := make([]ResolvedSetting, len(r.allSettings))
		copy(result, r.allSettings)

		return result
	}

	r.mu.RUnlock()

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.dirty {
		r.rebuildLocked()
	}

	result := make([]ResolvedSetting, len(r.allSettings))
	copy(result, r.allSettings)

	return result
}

// FindContributor returns the contributor with the given name.
func (r *ContributorRegistry) FindContributor(name string) (DashboardContributor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if c, ok := r.local[name]; ok {
		return c, true
	}

	if c, ok := r.ssr[name]; ok {
		return c, true
	}

	if c, ok := r.remote[name]; ok {
		return c, true
	}

	return nil, false
}

// FindLocalContributor returns the local contributor with the given name.
func (r *ContributorRegistry) FindLocalContributor(name string) (LocalContributor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.local[name]

	return c, ok
}

// IsLocal returns true if the named contributor is registered as a local contributor.
func (r *ContributorRegistry) IsLocal(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.local[name]

	return ok
}

// IsRemote returns true if the named contributor is registered as a remote contributor.
func (r *ContributorRegistry) IsRemote(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.remote[name]

	return ok
}

// FindRemoteContributor returns the remote contributor with the given name,
// type-asserting to *RemoteContributor. Returns false if not found or not a RemoteContributor.
func (r *ContributorRegistry) FindRemoteContributor(name string) (*RemoteContributor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.remote[name]
	if !ok {
		return nil, false
	}

	rc, ok := c.(*RemoteContributor)

	return rc, ok
}

// ContributorNames returns the names of all registered contributors.
func (r *ContributorRegistry) ContributorNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.local)+len(r.remote)+len(r.ssr))
	for name := range r.local {
		names = append(names, name)
	}

	for name := range r.ssr {
		names = append(names, name)
	}

	for name := range r.remote {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// ContributorCount returns the total number of registered contributors.
func (r *ContributorRegistry) ContributorCount() int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return len(r.local) + len(r.remote) + len(r.ssr)
}

// FindSSRContributor returns the SSR contributor with the given name.
func (r *ContributorRegistry) FindSSRContributor(name string) (*SSRContributor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	c, ok := r.ssr[name]

	return c, ok
}

// SSRContributorNames returns the names of all registered SSR contributors.
func (r *ContributorRegistry) SSRContributorNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.ssr))
	for name := range r.ssr {
		names = append(names, name)
	}

	sort.Strings(names)

	return names
}

// IsSSR returns true if the named contributor is registered as an SSR contributor.
func (r *ContributorRegistry) IsSSR(name string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	_, ok := r.ssr[name]

	return ok
}

// GetManifest returns the manifest for the named contributor.
func (r *ContributorRegistry) GetManifest(name string) (*Manifest, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	m, ok := r.manifests[name]

	return m, ok
}
