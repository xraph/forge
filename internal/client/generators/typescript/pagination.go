package typescript

import (
	"fmt"
	"strings"

	"github.com/xraph/forge/internal/client"
)

// PaginationGenerator generates pagination helpers for TypeScript.
type PaginationGenerator struct{}

// NewPaginationGenerator creates a new pagination generator.
func NewPaginationGenerator() *PaginationGenerator {
	return &PaginationGenerator{}
}

// DetectPaginatedEndpoints analyzes endpoints to identify pagination patterns.
func (p *PaginationGenerator) DetectPaginatedEndpoints(spec *client.APISpec) []client.Endpoint {
	var paginated []client.Endpoint

	for _, endpoint := range spec.Endpoints {
		if p.isPaginatedEndpoint(endpoint) {
			paginated = append(paginated, endpoint)
		}
	}

	return paginated
}

// isPaginatedEndpoint checks if an endpoint uses pagination.
func (p *PaginationGenerator) isPaginatedEndpoint(endpoint client.Endpoint) bool {
	// Check for x-pagination extension
	if paginationType, ok := endpoint.Metadata["x-pagination"].(string); ok && paginationType != "" {
		return true
	}

	// Check for common pagination parameters
	for _, param := range endpoint.QueryParams {
		paramName := strings.ToLower(param.Name)
		if paramName == "page" || paramName == "offset" || paramName == "cursor" ||
			paramName == "limit" || paramName == "per_page" || paramName == "page_size" {
			return true
		}
	}

	// Check response for pagination indicators
	for _, resp := range endpoint.Responses {
		for _, media := range resp.Content {
			if media.Schema != nil && p.hasListStructure(media.Schema) {
				return true
			}
		}
	}

	return false
}

// hasListStructure checks if schema has list/pagination structure.
func (p *PaginationGenerator) hasListStructure(schema *client.Schema) bool {
	if schema.Type != "object" || schema.Properties == nil {
		return false
	}

	// Check for common pagination fields
	hasData := false
	hasPagination := false

	for propName, prop := range schema.Properties {
		nameLower := strings.ToLower(propName)

		// Data field (usually array)
		if (nameLower == "data" || nameLower == "items" || nameLower == "results") &&
			prop.Type == "array" {
			hasData = true
		}

		// Pagination fields
		if nameLower == "next" || nameLower == "nextcursor" || nameLower == "nextpage" ||
			nameLower == "has_more" || nameLower == "hasmore" || nameLower == "total" {
			hasPagination = true
		}
	}

	return hasData && hasPagination
}

// GeneratePaginationHelpers generates pagination utility code.
func (p *PaginationGenerator) GeneratePaginationHelpers(spec *client.APISpec, config client.GeneratorConfig) string {
	var buf strings.Builder

	buf.WriteString("// Pagination helpers\n\n")

	// Pagination types
	buf.WriteString("export interface PaginationParams {\n")
	buf.WriteString("  limit?: number;\n")
	buf.WriteString("  offset?: number;\n")
	buf.WriteString("  cursor?: string;\n")
	buf.WriteString("  page?: number;\n")
	buf.WriteString("  pageSize?: number;\n")
	buf.WriteString("}\n\n")

	buf.WriteString("export interface PaginatedResponse<T> {\n")
	buf.WriteString("  data: T[];\n")
	buf.WriteString("  nextCursor?: string;\n")
	buf.WriteString("  prevCursor?: string;\n")
	buf.WriteString("  hasMore?: boolean;\n")
	buf.WriteString("  total?: number;\n")
	buf.WriteString("  page?: number;\n")
	buf.WriteString("  pageSize?: number;\n")
	buf.WriteString("}\n\n")

	// Async iterator helper
	buf.WriteString("/**\n")
	buf.WriteString(" * Creates an async iterator for paginated endpoints\n")
	buf.WriteString(" */\n")
	buf.WriteString("export async function* paginateAll<T>(\n")
	buf.WriteString("  fetchPage: (params: PaginationParams) => Promise<PaginatedResponse<T>>,\n")
	buf.WriteString("  initialParams: PaginationParams = {}\n")
	buf.WriteString("): AsyncGenerator<T, void, undefined> {\n")
	buf.WriteString("  let params = { ...initialParams };\n")
	buf.WriteString("  let hasMore = true;\n\n")

	buf.WriteString("  while (hasMore) {\n")
	buf.WriteString("    const response = await fetchPage(params);\n\n")

	buf.WriteString("    // Yield all items from current page\n")
	buf.WriteString("    for (const item of response.data) {\n")
	buf.WriteString("      yield item;\n")
	buf.WriteString("    }\n\n")

	buf.WriteString("    // Check if there are more pages\n")
	buf.WriteString("    hasMore = response.hasMore || false;\n\n")

	buf.WriteString("    // Update pagination params based on response\n")
	buf.WriteString("    if (response.nextCursor) {\n")
	buf.WriteString("      params.cursor = response.nextCursor;\n")
	buf.WriteString("    } else if (params.page !== undefined) {\n")
	buf.WriteString("      params.page++;\n")
	buf.WriteString("    } else if (params.offset !== undefined && params.limit) {\n")
	buf.WriteString("      params.offset += params.limit;\n")
	buf.WriteString("    } else {\n")
	buf.WriteString("      hasMore = false;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n")
	buf.WriteString("}\n\n")

	// Collect all helper
	buf.WriteString("/**\n")
	buf.WriteString(" * Collects all items from a paginated endpoint into an array\n")
	buf.WriteString(" */\n")
	buf.WriteString("export async function collectAll<T>(\n")
	buf.WriteString("  fetchPage: (params: PaginationParams) => Promise<PaginatedResponse<T>>,\n")
	buf.WriteString("  initialParams: PaginationParams = {},\n")
	buf.WriteString("  maxItems?: number\n")
	buf.WriteString("): Promise<T[]> {\n")
	buf.WriteString("  const items: T[] = [];\n")
	buf.WriteString("  let count = 0;\n\n")

	buf.WriteString("  for await (const item of paginateAll(fetchPage, initialParams)) {\n")
	buf.WriteString("    items.push(item);\n")
	buf.WriteString("    count++;\n\n")

	buf.WriteString("    if (maxItems && count >= maxItems) {\n")
	buf.WriteString("      break;\n")
	buf.WriteString("    }\n")
	buf.WriteString("  }\n\n")

	buf.WriteString("  return items;\n")
	buf.WriteString("}\n")

	return buf.String()
}

// GeneratePaginatedMethods generates paginated versions of methods.
func (p *PaginationGenerator) GeneratePaginatedMethods(endpoint client.Endpoint, spec *client.APISpec, config client.GeneratorConfig) string {
	if !p.isPaginatedEndpoint(endpoint) {
		return ""
	}

	var buf strings.Builder

	methodName := p.generateMethodName(endpoint)
	returnType := p.getArrayItemType(endpoint, spec)

	buf.WriteString(fmt.Sprintf("  /**\n   * Paginated version of %s - returns async iterator\n   */\n", methodName))
	buf.WriteString(fmt.Sprintf("  async *%sPaginated(\n", methodName))
	buf.WriteString("    params: PaginationParams = {},\n")
	buf.WriteString("    options?: { signal?: AbortSignal }\n")
	buf.WriteString(fmt.Sprintf("  ): AsyncGenerator<%s, void, undefined> {\n", returnType))
	buf.WriteString("    yield* paginateAll(\n")
	buf.WriteString(fmt.Sprintf("      (p) => this.%s({ ...params, ...p }, options),\n", methodName))
	buf.WriteString("      params\n")
	buf.WriteString("    );\n")
	buf.WriteString("  }\n\n")

	buf.WriteString(fmt.Sprintf("  /**\n   * Collect all items from %s\n   */\n", methodName))
	buf.WriteString(fmt.Sprintf("  async %sAll(\n", methodName))
	buf.WriteString("    params: PaginationParams = {},\n")
	buf.WriteString("    maxItems?: number,\n")
	buf.WriteString("    options?: { signal?: AbortSignal }\n")
	buf.WriteString(fmt.Sprintf("  ): Promise<%s[]> {\n", returnType))
	buf.WriteString("    return collectAll(\n")
	buf.WriteString(fmt.Sprintf("      (p) => this.%s({ ...params, ...p }, options),\n", methodName))
	buf.WriteString("      params,\n")
	buf.WriteString("      maxItems\n")
	buf.WriteString("    );\n")
	buf.WriteString("  }\n")

	return buf.String()
}

// generateMethodName generates method name from endpoint.
func (p *PaginationGenerator) generateMethodName(endpoint client.Endpoint) string {
	if endpoint.OperationID != "" {
		return p.toCamelCase(endpoint.OperationID)
	}

	path := strings.TrimPrefix(endpoint.Path, "/")
	path = strings.ReplaceAll(path, "/", "_")
	path = strings.ReplaceAll(path, "{", "")
	path = strings.ReplaceAll(path, "}", "")
	path = strings.ReplaceAll(path, "-", "_")
	method := strings.ToLower(endpoint.Method)

	return p.toCamelCase(method + "_" + path)
}

// getArrayItemType extracts the item type from a paginated response.
func (p *PaginationGenerator) getArrayItemType(endpoint client.Endpoint, spec *client.APISpec) string {
	// Look for 200 response
	if resp, ok := endpoint.Responses[200]; ok {
		if media, ok := resp.Content["application/json"]; ok && media.Schema != nil {
			if media.Schema.Properties != nil {
				// Look for data/items/results array
				for propName, prop := range media.Schema.Properties {
					nameLower := strings.ToLower(propName)
					if (nameLower == "data" || nameLower == "items" || nameLower == "results") &&
						prop.Type == "array" && prop.Items != nil {
						return p.getTypeName(prop.Items, spec)
					}
				}
			}
		}
	}

	return "any"
}

// getTypeName gets the TypeScript type name for a schema.
func (p *PaginationGenerator) getTypeName(schema *client.Schema, spec *client.APISpec) string {
	if schema == nil {
		return "any"
	}

	if schema.Ref != "" {
		parts := strings.Split(schema.Ref, "/")

		return parts[len(parts)-1]
	}

	switch schema.Type {
	case "string":
		return "string"
	case "number", "integer":
		return "number"
	case "boolean":
		return "boolean"
	case "array":
		if schema.Items != nil {
			return p.getTypeName(schema.Items, spec) + "[]"
		}

		return "any[]"
	case "object":
		return "Record<string, any>"
	}

	return "any"
}

// toCamelCase converts a string to camelCase.
func (p *PaginationGenerator) toCamelCase(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
		return r == '_' || r == '-' || r == ' '
	})

	if len(parts) == 0 {
		return s
	}

	result := strings.ToLower(parts[0])

	var resultSb291 strings.Builder

	for i := 1; i < len(parts); i++ {
		if len(parts[i]) > 0 {
			resultSb291.WriteString(strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:]))
		}
	}

	result += resultSb291.String()

	return result
}
