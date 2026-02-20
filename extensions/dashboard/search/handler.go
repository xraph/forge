package search

import (
	"github.com/xraph/forge"
)

// HandleSearchAPI returns a handler that serves search results as JSON.
// GET {base}/api/search?q=...&limit=20.
func HandleSearchAPI(searcher *FederatedSearch) forge.Handler {
	return func(ctx forge.Context) error {
		query := ctx.Query("q")
		if query == "" {
			return ctx.JSON(200, map[string]any{
				"results": []any{},
				"query":   "",
				"total":   0,
			})
		}

		limit := 20
		// Could parse limit from query params if desired

		results := searcher.Search(ctx.Context(), query, limit)

		return ctx.JSON(200, map[string]any{
			"results": results,
			"query":   query,
			"total":   len(results),
		})
	}
}
