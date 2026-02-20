package settings

import (
	"sort"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// GroupedSettings represents settings organized by group.
type GroupedSettings struct {
	Group    string
	Settings []contributor.ResolvedSetting
}

// Aggregator collects and organizes settings from all contributors.
type Aggregator struct {
	registry *contributor.ContributorRegistry
}

// NewAggregator creates a new settings aggregator.
func NewAggregator(registry *contributor.ContributorRegistry) *Aggregator {
	return &Aggregator{registry: registry}
}

// GetAllGrouped returns all settings organized by group.
func (a *Aggregator) GetAllGrouped() []GroupedSettings {
	all := a.registry.GetAllSettings()
	if len(all) == 0 {
		return nil
	}

	groupMap := make(map[string][]contributor.ResolvedSetting)

	for _, s := range all {
		group := s.Group
		if group == "" {
			group = "General"
		}

		groupMap[group] = append(groupMap[group], s)
	}

	var groups []GroupedSettings

	for name, settings := range groupMap {
		// Sort within group by priority
		sort.Slice(settings, func(i, j int) bool {
			return settings[i].Priority < settings[j].Priority
		})
		groups = append(groups, GroupedSettings{
			Group:    name,
			Settings: settings,
		})
	}

	// Sort groups alphabetically
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].Group < groups[j].Group
	})

	return groups
}

// GetForContributor returns settings for a specific contributor.
func (a *Aggregator) GetForContributor(name string) []contributor.ResolvedSetting {
	all := a.registry.GetAllSettings()

	var result []contributor.ResolvedSetting

	for _, s := range all {
		if s.Contributor == name {
			result = append(result, s)
		}
	}

	return result
}
