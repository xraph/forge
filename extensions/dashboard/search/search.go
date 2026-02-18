package search

import (
	"context"
	"sort"
	"sync"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// FederatedSearch fans out search queries across all SearchableContributor
// instances and the local content index, then merges and ranks results.
type FederatedSearch struct {
	registry *contributor.ContributorRegistry
	index    *Index
	basePath string
	logger   forge.Logger
}

// NewFederatedSearch creates a new federated search engine.
func NewFederatedSearch(
	registry *contributor.ContributorRegistry,
	basePath string,
	logger forge.Logger,
) *FederatedSearch {
	return &FederatedSearch{
		registry: registry,
		index:    NewIndex(),
		basePath: basePath,
		logger:   logger,
	}
}

// RebuildIndex rebuilds the local search index from the registry.
func (fs *FederatedSearch) RebuildIndex() {
	fs.index.Rebuild(fs.registry, fs.basePath)
}

// Search performs a federated search across all providers.
func (fs *FederatedSearch) Search(ctx context.Context, query string, limit int) []contributor.SearchResult {
	if query == "" {
		return nil
	}

	if limit <= 0 {
		limit = 20
	}

	// Collect results from all sources concurrently
	var mu sync.Mutex
	var allResults []contributor.SearchResult

	var wg sync.WaitGroup

	// 1. Search local index
	wg.Add(1)
	go func() {
		defer wg.Done()
		localResults := fs.index.Search(query, limit)
		mu.Lock()
		allResults = append(allResults, localResults...)
		mu.Unlock()
	}()

	// 2. Fan out to SearchableContributors
	for _, name := range fs.registry.ContributorNames() {
		c, ok := fs.registry.FindContributor(name)
		if !ok {
			continue
		}

		searchable, ok := c.(contributor.SearchableContributor)
		if !ok {
			continue
		}

		wg.Add(1)
		go func(s contributor.SearchableContributor, contributorName string) {
			defer wg.Done()

			results, err := s.Search(ctx, query, limit)
			if err != nil {
				fs.logger.Debug("search provider error",
					forge.F("contributor", contributorName),
					forge.F("error", err.Error()),
				)
				return
			}

			// Tag results with source
			for i := range results {
				if results[i].Source == "" {
					results[i].Source = contributorName
				}
			}

			mu.Lock()
			allResults = append(allResults, results...)
			mu.Unlock()
		}(searchable, name)
	}

	wg.Wait()

	// Sort by score descending
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})

	// Truncate to limit
	if len(allResults) > limit {
		allResults = allResults[:limit]
	}

	return allResults
}

// Index returns the local search index for direct access.
func (fs *FederatedSearch) Index() *Index {
	return fs.index
}
