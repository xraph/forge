package golang

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// PaginationGenerator generates pagination helpers for Go.
type PaginationGenerator struct{}

// NewPaginationGenerator creates a new pagination generator.
func NewPaginationGenerator() *PaginationGenerator {
	return &PaginationGenerator{}
}

// Generate generates pagination helper code.
func (p *PaginationGenerator) Generate(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString(fmt.Sprintf("package %s\n\n", config.PackageName))

	buf.WriteString("import (\n")
	buf.WriteString("\t\"context\"\n")
	buf.WriteString(")\n\n")

	// Pagination parameters
	buf.WriteString("// PaginationParams contains pagination parameters.\n")
	buf.WriteString("type PaginationParams struct {\n")
	buf.WriteString("\tLimit  *int    `json:\"limit,omitempty\"`\n")
	buf.WriteString("\tOffset *int    `json:\"offset,omitempty\"`\n")
	buf.WriteString("\tCursor *string `json:\"cursor,omitempty\"`\n")
	buf.WriteString("\tPage   *int    `json:\"page,omitempty\"`\n")
	buf.WriteString("\tPageSize *int  `json:\"pageSize,omitempty\"`\n")
	buf.WriteString("}\n\n")

	// Paginated response
	buf.WriteString("// PaginatedResponse represents a paginated API response.\n")
	buf.WriteString("type PaginatedResponse[T any] struct {\n")
	buf.WriteString("\tData       []T     `json:\"data\"`\n")
	buf.WriteString("\tNextCursor *string `json:\"nextCursor,omitempty\"`\n")
	buf.WriteString("\tPrevCursor *string `json:\"prevCursor,omitempty\"`\n")
	buf.WriteString("\tHasMore    *bool   `json:\"hasMore,omitempty\"`\n")
	buf.WriteString("\tTotal      *int    `json:\"total,omitempty\"`\n")
	buf.WriteString("\tPage       *int    `json:\"page,omitempty\"`\n")
	buf.WriteString("\tPageSize   *int    `json:\"pageSize,omitempty\"`\n")
	buf.WriteString("}\n\n")

	// Iterator type
	buf.WriteString("// PaginationIterator provides iteration over paginated results.\n")
	buf.WriteString("type PaginationIterator[T any] struct {\n")
	buf.WriteString("\tfetchPage func(ctx context.Context, params PaginationParams) (*PaginatedResponse[T], error)\n")
	buf.WriteString("\tparams    PaginationParams\n")
	buf.WriteString("\tcurrent   []T\n")
	buf.WriteString("\tindex     int\n")
	buf.WriteString("\thasMore   bool\n")
	buf.WriteString("\terr       error\n")
	buf.WriteString("}\n\n")

	// NewPaginationIterator
	buf.WriteString("// NewPaginationIterator creates a new pagination iterator.\n")
	buf.WriteString("func NewPaginationIterator[T any](\n")
	buf.WriteString("\tfetchPage func(ctx context.Context, params PaginationParams) (*PaginatedResponse[T], error),\n")
	buf.WriteString("\tinitialParams PaginationParams,\n")
	buf.WriteString(") *PaginationIterator[T] {\n")
	buf.WriteString("\treturn &PaginationIterator[T]{\n")
	buf.WriteString("\t\tfetchPage: fetchPage,\n")
	buf.WriteString("\t\tparams:    initialParams,\n")
	buf.WriteString("\t\thasMore:   true,\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	// Next method
	buf.WriteString("// Next advances the iterator and returns the next item.\n")
	buf.WriteString("func (it *PaginationIterator[T]) Next(ctx context.Context) (T, bool, error) {\n")
	buf.WriteString("\tvar zero T\n\n")

	buf.WriteString("\t// Check if we need to fetch the next page\n")
	buf.WriteString("\tif it.index >= len(it.current) {\n")
	buf.WriteString("\t\tif !it.hasMore {\n")
	buf.WriteString("\t\t\treturn zero, false, it.err\n")
	buf.WriteString("\t\t}\n\n")

	buf.WriteString("\t\t// Fetch next page\n")
	buf.WriteString("\t\tresp, err := it.fetchPage(ctx, it.params)\n")
	buf.WriteString("\t\tif err != nil {\n")
	buf.WriteString("\t\t\tit.err = err\n")
	buf.WriteString("\t\t\treturn zero, false, err\n")
	buf.WriteString("\t\t}\n\n")

	buf.WriteString("\t\tit.current = resp.Data\n")
	buf.WriteString("\t\tit.index = 0\n\n")

	buf.WriteString("\t\t// Check if there are more pages\n")
	buf.WriteString("\t\tif resp.HasMore != nil {\n")
	buf.WriteString("\t\t\tit.hasMore = *resp.HasMore\n")
	buf.WriteString("\t\t} else if resp.NextCursor != nil {\n")
	buf.WriteString("\t\t\tit.hasMore = *resp.NextCursor != \"\"\n")
	buf.WriteString("\t\t} else {\n")
	buf.WriteString("\t\t\tit.hasMore = false\n")
	buf.WriteString("\t\t}\n\n")

	buf.WriteString("\t\t// Update pagination params\n")
	buf.WriteString("\t\tif resp.NextCursor != nil {\n")
	buf.WriteString("\t\t\tit.params.Cursor = resp.NextCursor\n")
	buf.WriteString("\t\t} else if it.params.Page != nil {\n")
	buf.WriteString("\t\t\tnextPage := *it.params.Page + 1\n")
	buf.WriteString("\t\t\tit.params.Page = &nextPage\n")
	buf.WriteString("\t\t} else if it.params.Offset != nil && it.params.Limit != nil {\n")
	buf.WriteString("\t\t\tnextOffset := *it.params.Offset + *it.params.Limit\n")
	buf.WriteString("\t\t\tit.params.Offset = &nextOffset\n")
	buf.WriteString("\t\t}\n\n")

	buf.WriteString("\t\t// If no items in current page, we're done\n")
	buf.WriteString("\t\tif len(it.current) == 0 {\n")
	buf.WriteString("\t\t\treturn zero, false, nil\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\t// Return next item\n")
	buf.WriteString("\titem := it.current[it.index]\n")
	buf.WriteString("\tit.index++\n")
	buf.WriteString("\treturn item, true, nil\n")
	buf.WriteString("}\n\n")

	// CollectAll helper
	buf.WriteString("// CollectAll collects all items from a paginated endpoint into a slice.\n")
	buf.WriteString("func CollectAll[T any](\n")
	buf.WriteString("\tctx context.Context,\n")
	buf.WriteString("\tfetchPage func(ctx context.Context, params PaginationParams) (*PaginatedResponse[T], error),\n")
	buf.WriteString("\tinitialParams PaginationParams,\n")
	buf.WriteString("\tmaxItems int,\n")
	buf.WriteString(") ([]T, error) {\n")
	buf.WriteString("\tvar items []T\n")
	buf.WriteString("\titerator := NewPaginationIterator(fetchPage, initialParams)\n\n")

	buf.WriteString("\tfor {\n")
	buf.WriteString("\t\titem, hasNext, err := iterator.Next(ctx)\n")
	buf.WriteString("\t\tif err != nil {\n")
	buf.WriteString("\t\t\treturn items, err\n")
	buf.WriteString("\t\t}\n\n")

	buf.WriteString("\t\tif !hasNext {\n")
	buf.WriteString("\t\t\tbreak\n")
	buf.WriteString("\t\t}\n\n")

	buf.WriteString("\t\titems = append(items, item)\n\n")

	buf.WriteString("\t\tif maxItems > 0 && len(items) >= maxItems {\n")
	buf.WriteString("\t\t\tbreak\n")
	buf.WriteString("\t\t}\n")
	buf.WriteString("\t}\n\n")

	buf.WriteString("\treturn items, nil\n")
	buf.WriteString("}\n")

	return buf.String()
}

