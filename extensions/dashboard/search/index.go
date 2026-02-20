package search

import (
	"strings"
	"sync"

	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// IndexEntry represents a searchable item in the local index.
type IndexEntry struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Icon        string `json:"icon"`
	Source      string `json:"source"`
	Category    string `json:"category"` // "page", "widget", "setting"
}

// Index provides fast in-memory search over local dashboard content.
type Index struct {
	mu      sync.RWMutex
	entries []IndexEntry
}

// NewIndex creates a new search index.
func NewIndex() *Index {
	return &Index{}
}

// Rebuild rebuilds the index from the registry's current state.
func (idx *Index) Rebuild(registry *contributor.ContributorRegistry, basePath string) {
	idx.mu.Lock()
	defer idx.mu.Unlock()

	var entries []IndexEntry

	// Index nav items
	for _, group := range registry.GetNavGroups() {
		for _, item := range group.Items {
			entries = append(entries, IndexEntry{
				Title:    item.Label,
				URL:      item.FullPath,
				Icon:     item.Icon,
				Source:   item.Contributor,
				Category: "page",
			})
		}
	}

	// Index widgets
	for _, w := range registry.GetAllWidgets() {
		entries = append(entries, IndexEntry{
			Title:       w.Title,
			Description: w.Description,
			URL:         basePath + "/ext/" + w.Contributor + "/widgets/" + w.ID,
			Icon:        "layout-grid",
			Source:      w.Contributor,
			Category:    "widget",
		})
	}

	// Index settings
	for _, s := range registry.GetAllSettings() {
		entries = append(entries, IndexEntry{
			Title:       s.Title,
			Description: s.Description,
			URL:         basePath + "/ext/" + s.Contributor + "/settings/" + s.ID,
			Icon:        s.Icon,
			Source:      s.Contributor,
			Category:    "setting",
		})
	}

	idx.entries = entries
}

// Search performs a simple substring match against indexed entries.
func (idx *Index) Search(query string, limit int) []contributor.SearchResult {
	idx.mu.RLock()
	defer idx.mu.RUnlock()

	query = strings.ToLower(query)

	var results []contributor.SearchResult

	for _, entry := range idx.entries {
		if limit > 0 && len(results) >= limit {
			break
		}

		titleLower := strings.ToLower(entry.Title)
		descLower := strings.ToLower(entry.Description)

		// Score based on match quality
		var score float64

		switch {
		case strings.EqualFold(entry.Title, query):
			score = 1.0 // exact match
		case strings.HasPrefix(titleLower, query):
			score = 0.8 // prefix match
		case strings.Contains(titleLower, query):
			score = 0.6 // title contains
		case strings.Contains(descLower, query):
			score = 0.3 // description contains
		default:
			continue // no match
		}

		results = append(results, contributor.SearchResult{
			Title:       entry.Title,
			Description: entry.Description,
			URL:         entry.URL,
			Icon:        entry.Icon,
			Source:      entry.Source,
			Category:    entry.Category,
			Score:       score,
		})
	}

	return results
}
