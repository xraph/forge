package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/xraph/forge"
)

// OpenAPIAggregator fetches, caches, and merges OpenAPI specs from all
// discovered upstream services (via FARP metadata). It exposes:
//   - A unified merged OpenAPI spec combining all service paths under gateway prefixes
//   - Per-service OpenAPI specs fetched and cached from upstream endpoints
//   - A Swagger UI for browsing the aggregated spec
//
// Schema fetching uses the `farp.openapi` metadata key set by the discovery
// extension's SchemaPublisher. The aggregator periodically refreshes specs
// and supports on-demand refresh.
type OpenAPIAggregator struct {
	config     OpenAPIConfig
	logger     forge.Logger
	rm         *RouteManager
	disc       *ServiceDiscovery
	httpClient *http.Client

	mu             sync.RWMutex
	serviceSpecs   map[string]*ServiceOpenAPISpec // serviceName -> spec
	mergedSpec     map[string]any                 // cached merged spec
	mergedSpecJSON []byte                         // pre-serialized JSON
	lastRefresh    time.Time
	refreshing     bool
}

// ServiceOpenAPISpec holds a cached OpenAPI spec for a single upstream service.
type ServiceOpenAPISpec struct {
	ServiceName string         `json:"serviceName"`
	Version     string         `json:"version"`
	SpecURL     string         `json:"specUrl"`
	Spec        map[string]any `json:"spec,omitempty"`
	FetchedAt   time.Time      `json:"fetchedAt"`
	Error       string         `json:"error,omitempty"`
	Healthy     bool           `json:"healthy"`
	PathCount   int            `json:"pathCount"`
}

// OpenAPIConfig holds configuration for the OpenAPI aggregation feature.
type OpenAPIConfig struct {
	// Enabled turns on/off the OpenAPI aggregation feature
	Enabled bool `json:"enabled" yaml:"enabled"`

	// Path is the endpoint path to serve the aggregated OpenAPI spec
	Path string `json:"path" yaml:"path"`

	// UIPath is the endpoint path to serve the Swagger UI
	UIPath string `json:"uiPath" yaml:"ui_path"`

	// Title is the title for the aggregated spec
	Title string `json:"title" yaml:"title"`

	// Description is the description for the aggregated spec
	Description string `json:"description" yaml:"description"`

	// Version is the version for the aggregated spec
	Version string `json:"version" yaml:"version"`

	// RefreshInterval is how often to re-fetch upstream specs
	RefreshInterval time.Duration `json:"refreshInterval" yaml:"refresh_interval"`

	// FetchTimeout is the timeout for fetching a single upstream spec
	FetchTimeout time.Duration `json:"fetchTimeout" yaml:"fetch_timeout"`

	// StripServicePrefix controls whether service prefixes are included in paths
	StripServicePrefix bool `json:"stripServicePrefix" yaml:"strip_service_prefix"`

	// MergeStrategy controls how conflicting paths are handled:
	// "prefix" (default) - prefix all paths with /{serviceName}
	// "flat" - merge paths as-is, last wins on conflict
	MergeStrategy string `json:"mergeStrategy" yaml:"merge_strategy"`

	// IncludeGatewayRoutes includes the gateway's own admin routes in the spec
	IncludeGatewayRoutes bool `json:"includeGatewayRoutes" yaml:"include_gateway_routes"`

	// ExcludeServices is a list of service names to exclude from aggregation
	ExcludeServices []string `json:"excludeServices,omitempty" yaml:"exclude_services"`

	// ContactName is the contact name for the spec info
	ContactName string `json:"contactName,omitempty" yaml:"contact_name"`

	// ContactEmail is the contact email for the spec info
	ContactEmail string `json:"contactEmail,omitempty" yaml:"contact_email"`
}

// DefaultOpenAPIConfig returns defaults for OpenAPI aggregation.
func DefaultOpenAPIConfig() OpenAPIConfig {
	return OpenAPIConfig{
		Enabled:         true,
		Path:            "/openapi.json",
		UIPath:          "/swagger",
		Title:           "API Gateway",
		Description:     "Aggregated API specification from all upstream services",
		Version:         "1.0.0",
		RefreshInterval: 30 * time.Second,
		FetchTimeout:    10 * time.Second,
		MergeStrategy:   "prefix",
	}
}

// NewOpenAPIAggregator creates a new OpenAPI aggregator.
func NewOpenAPIAggregator(config OpenAPIConfig, logger forge.Logger, rm *RouteManager, disc *ServiceDiscovery) *OpenAPIAggregator {
	return &OpenAPIAggregator{
		config: config,
		logger: logger,
		rm:     rm,
		disc:   disc,
		httpClient: &http.Client{
			Timeout: config.FetchTimeout,
		},
		serviceSpecs: make(map[string]*ServiceOpenAPISpec),
	}
}

// Start begins the periodic spec refresh loop.
func (oa *OpenAPIAggregator) Start(ctx context.Context) {
	if !oa.config.Enabled {
		return
	}

	// Initial fetch
	oa.Refresh(ctx)

	// Periodic refresh
	go func() {
		ticker := time.NewTicker(oa.config.RefreshInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				oa.Refresh(ctx)
			}
		}
	}()

	oa.logger.Info("OpenAPI aggregator started",
		forge.F("path", oa.config.Path),
		forge.F("ui_path", oa.config.UIPath),
		forge.F("refresh_interval", oa.config.RefreshInterval),
	)
}

// Refresh fetches all upstream OpenAPI specs and rebuilds the merged spec.
func (oa *OpenAPIAggregator) Refresh(ctx context.Context) {
	oa.mu.Lock()
	if oa.refreshing {
		oa.mu.Unlock()
		return
	}
	oa.refreshing = true
	oa.mu.Unlock()

	defer func() {
		oa.mu.Lock()
		oa.refreshing = false
		oa.mu.Unlock()
	}()

	// Discover services with OpenAPI endpoints
	services := oa.discoverOpenAPIServices()

	// Fetch specs in parallel
	var wg sync.WaitGroup
	specCh := make(chan *ServiceOpenAPISpec, len(services))

	for _, svc := range services {
		wg.Add(1)
		go func(s discoveredOpenAPIService) {
			defer wg.Done()
			spec := oa.fetchServiceSpec(ctx, s)
			specCh <- spec
		}(svc)
	}

	wg.Wait()
	close(specCh)

	// Collect results
	newSpecs := make(map[string]*ServiceOpenAPISpec)
	for spec := range specCh {
		newSpecs[spec.ServiceName] = spec
	}

	// Build merged spec
	merged := oa.buildMergedSpec(newSpecs)

	// Serialize to JSON
	mergedJSON, err := json.Marshal(merged)
	if err != nil {
		oa.logger.Error("failed to serialize merged OpenAPI spec", forge.F("error", err))
		return
	}

	// Atomically update
	oa.mu.Lock()
	oa.serviceSpecs = newSpecs
	oa.mergedSpec = merged
	oa.mergedSpecJSON = mergedJSON
	oa.lastRefresh = time.Now()
	oa.mu.Unlock()

	oa.logger.Debug("OpenAPI specs refreshed",
		forge.F("services", len(newSpecs)),
		forge.F("total_paths", countPaths(merged)),
	)
}

// MergedSpec returns the pre-serialized merged OpenAPI spec JSON.
func (oa *OpenAPIAggregator) MergedSpec() []byte {
	oa.mu.RLock()
	defer oa.mu.RUnlock()
	return oa.mergedSpecJSON
}

// MergedSpecMap returns the merged spec as a map.
func (oa *OpenAPIAggregator) MergedSpecMap() map[string]any {
	oa.mu.RLock()
	defer oa.mu.RUnlock()
	return oa.mergedSpec
}

// ServiceSpec returns the cached spec for a specific service.
func (oa *OpenAPIAggregator) ServiceSpec(serviceName string) *ServiceOpenAPISpec {
	oa.mu.RLock()
	defer oa.mu.RUnlock()
	return oa.serviceSpecs[serviceName]
}

// ServiceSpecs returns all cached service specs.
func (oa *OpenAPIAggregator) ServiceSpecs() map[string]*ServiceOpenAPISpec {
	oa.mu.RLock()
	defer oa.mu.RUnlock()

	// Return a copy
	result := make(map[string]*ServiceOpenAPISpec, len(oa.serviceSpecs))
	for k, v := range oa.serviceSpecs {
		result[k] = v
	}

	return result
}

// LastRefresh returns the time of the last spec refresh.
func (oa *OpenAPIAggregator) LastRefresh() time.Time {
	oa.mu.RLock()
	defer oa.mu.RUnlock()
	return oa.lastRefresh
}

// discoveredOpenAPIService holds info about a service with an OpenAPI endpoint.
type discoveredOpenAPIService struct {
	Name       string
	Version    string
	SpecURL    string
	RouteCount int
}

// discoverOpenAPIServices finds all services that expose OpenAPI specs.
func (oa *OpenAPIAggregator) discoverOpenAPIServices() []discoveredOpenAPIService {
	var services []discoveredOpenAPIService

	// Get discovered services from the discovery component
	if oa.disc != nil {
		for _, svc := range oa.disc.DiscoveredServices() {
			// Check if this service is excluded
			if oa.isExcluded(svc.Name) {
				continue
			}

			// Look for OpenAPI endpoint in metadata
			specURL := ""
			if url, ok := svc.Metadata["farp.openapi"]; ok && url != "" {
				specURL = url
			} else if path, ok := svc.Metadata["farp.openapi.path"]; ok && path != "" {
				// Build full URL from service address
				specURL = fmt.Sprintf("http://%s:%d%s", svc.Address, svc.Port, path)
			}

			if specURL == "" {
				continue
			}

			services = append(services, discoveredOpenAPIService{
				Name:       svc.Name,
				Version:    svc.Version,
				SpecURL:    specURL,
				RouteCount: svc.RouteCount,
			})
		}
	}

	// Also check route-level metadata for non-FARP services that still expose OpenAPI
	routes := oa.rm.ListRoutes()
	knownServices := make(map[string]bool)
	for _, s := range services {
		knownServices[s.Name] = true
	}

	for _, route := range routes {
		if route.ServiceName == "" || knownServices[route.ServiceName] {
			continue
		}

		if oa.isExcluded(route.ServiceName) {
			continue
		}

		// Check if any target has an OpenAPI endpoint
		for _, target := range route.Targets {
			if openapiPath, ok := target.Metadata["openapi"]; ok && openapiPath != "" {
				services = append(services, discoveredOpenAPIService{
					Name:    route.ServiceName,
					SpecURL: target.URL + openapiPath,
				})
				knownServices[route.ServiceName] = true

				break
			}
		}
	}

	return services
}

// fetchServiceSpec fetches the OpenAPI spec from a single upstream service.
func (oa *OpenAPIAggregator) fetchServiceSpec(ctx context.Context, svc discoveredOpenAPIService) *ServiceOpenAPISpec {
	result := &ServiceOpenAPISpec{
		ServiceName: svc.Name,
		Version:     svc.Version,
		SpecURL:     svc.SpecURL,
		FetchedAt:   time.Now(),
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, svc.SpecURL, nil)
	if err != nil {
		result.Error = fmt.Sprintf("failed to create request: %v", err)
		oa.logger.Debug("failed to create OpenAPI fetch request",
			forge.F("service", svc.Name),
			forge.F("url", svc.SpecURL),
			forge.F("error", err),
		)
		return result
	}

	req.Header.Set("Accept", "application/json")

	resp, err := oa.httpClient.Do(req)
	if err != nil {
		result.Error = fmt.Sprintf("fetch failed: %v", err)
		oa.logger.Debug("failed to fetch OpenAPI spec",
			forge.F("service", svc.Name),
			forge.F("url", svc.SpecURL),
			forge.F("error", err),
		)
		return result
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
		oa.logger.Debug("non-200 response for OpenAPI spec",
			forge.F("service", svc.Name),
			forge.F("status", resp.StatusCode),
		)
		return result
	}

	// Read with a size limit (10MB max)
	body, err := io.ReadAll(io.LimitReader(resp.Body, 10*1024*1024))
	if err != nil {
		result.Error = fmt.Sprintf("read failed: %v", err)
		return result
	}

	// Parse the spec
	var spec map[string]any
	if err := json.Unmarshal(body, &spec); err != nil {
		result.Error = fmt.Sprintf("invalid JSON: %v", err)
		oa.logger.Debug("failed to parse OpenAPI spec",
			forge.F("service", svc.Name),
			forge.F("error", err),
		)
		return result
	}

	result.Spec = spec
	result.Healthy = true
	result.PathCount = countPaths(spec)

	oa.logger.Debug("fetched OpenAPI spec",
		forge.F("service", svc.Name),
		forge.F("paths", result.PathCount),
	)

	return result
}

// buildMergedSpec builds a unified OpenAPI 3.1.0 spec from all service specs.
func (oa *OpenAPIAggregator) buildMergedSpec(specs map[string]*ServiceOpenAPISpec) map[string]any {
	mergedPaths := make(map[string]any)
	mergedTags := make([]any, 0)
	mergedSchemas := make(map[string]any)
	mergedSecuritySchemes := make(map[string]any)
	serviceNames := make([]string, 0, len(specs))

	// Sort service names for deterministic output
	for name := range specs {
		serviceNames = append(serviceNames, name)
	}
	sort.Strings(serviceNames)

	tagSet := make(map[string]bool)

	for _, name := range serviceNames {
		svcSpec := specs[name]
		if svcSpec.Spec == nil || svcSpec.Error != "" {
			continue
		}

		spec := svcSpec.Spec

		// Extract paths
		paths, _ := spec["paths"].(map[string]any)

		// Determine the prefix for this service's paths
		prefix := ""
		if oa.config.MergeStrategy == "prefix" {
			// Use the gateway's configured prefix for this service
			if oa.disc != nil {
				prefix = oa.disc.buildPrefix(name)
			} else {
				prefix = "/" + name
			}
		}

		// Add a service-level tag
		serviceTag := name
		if !tagSet[serviceTag] {
			tagDesc := fmt.Sprintf("Operations from %s", name)
			if svcSpec.Version != "" {
				tagDesc += " v" + svcSpec.Version
			}
			mergedTags = append(mergedTags, map[string]any{
				"name":        serviceTag,
				"description": tagDesc,
			})
			tagSet[serviceTag] = true
		}

		// Merge paths
		for path, pathItem := range paths {
			mergedPath := prefix + path
			if mergedPath == "" {
				mergedPath = "/"
			}

			// Tag all operations with the service name
			taggedPathItem := tagOperations(pathItem, serviceTag)

			if _, exists := mergedPaths[mergedPath]; exists && oa.config.MergeStrategy == "prefix" {
				// Prefix strategy shouldn't have conflicts, but handle gracefully
				oa.logger.Debug("path conflict in merged spec",
					forge.F("path", mergedPath),
					forge.F("service", name),
				)
			}

			mergedPaths[mergedPath] = taggedPathItem
		}

		// Merge components/schemas (namespace to avoid conflicts)
		if components, ok := spec["components"].(map[string]any); ok {
			if schemas, ok := components["schemas"].(map[string]any); ok {
				for schemaName, schema := range schemas {
					// Namespace schema names by service to avoid conflicts
					namespacedName := name + "." + schemaName
					mergedSchemas[namespacedName] = schema

					// Also need to update $ref pointers in paths
					// This is handled by rewriteRefs below
				}
			}

			if secSchemes, ok := components["securitySchemes"].(map[string]any); ok {
				for schemeName, scheme := range secSchemes {
					namespacedName := name + "." + schemeName
					mergedSecuritySchemes[namespacedName] = scheme
				}
			}
		}
	}

	// Rewrite $ref pointers in merged paths to use namespaced schema names
	for serviceName := range serviceNames {
		name := serviceNames[serviceName]
		rewriteRefs(mergedPaths, name)
	}

	// Build the merged spec
	merged := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       oa.config.Title,
			"description": oa.config.Description,
			"version":     oa.config.Version,
		},
		"paths": mergedPaths,
		"tags":  mergedTags,
	}

	// Add contact info if configured
	if oa.config.ContactName != "" || oa.config.ContactEmail != "" {
		info := merged["info"].(map[string]any)
		contact := make(map[string]any)
		if oa.config.ContactName != "" {
			contact["name"] = oa.config.ContactName
		}
		if oa.config.ContactEmail != "" {
			contact["email"] = oa.config.ContactEmail
		}
		info["contact"] = contact
	}

	// Add components if we have any
	if len(mergedSchemas) > 0 || len(mergedSecuritySchemes) > 0 {
		components := make(map[string]any)
		if len(mergedSchemas) > 0 {
			components["schemas"] = mergedSchemas
		}
		if len(mergedSecuritySchemes) > 0 {
			components["securitySchemes"] = mergedSecuritySchemes
		}
		merged["components"] = components
	}

	// Add gateway routes if configured
	if oa.config.IncludeGatewayRoutes {
		oa.addGatewayRoutes(mergedPaths, &mergedTags, tagSet)
		merged["paths"] = mergedPaths
		merged["tags"] = mergedTags
	}

	// Add x-gateway metadata
	merged["x-gateway"] = map[string]any{
		"generatedAt":     time.Now().UTC().Format(time.RFC3339),
		"serviceCount":    len(specs),
		"pathCount":       len(mergedPaths),
		"refreshInterval": oa.config.RefreshInterval.String(),
	}

	return merged
}

// addGatewayRoutes adds the gateway's own admin API routes to the merged spec.
func (oa *OpenAPIAggregator) addGatewayRoutes(paths map[string]any, tags *[]any, tagSet map[string]bool) {
	if !tagSet["gateway"] {
		*tags = append(*tags, map[string]any{
			"name":        "gateway",
			"description": "Gateway administration API",
		})
		tagSet["gateway"] = true
	}

	gatewayRoutes := map[string]map[string]any{
		"/gateway/api/routes": {
			"get": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "List all gateway routes",
				"operationId": "listRoutes",
				"parameters": []map[string]any{
					{"name": "source", "in": "query", "schema": map[string]any{"type": "string"}},
					{"name": "protocol", "in": "query", "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "List of routes"},
				},
			},
			"post": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "Create a new route",
				"operationId": "createRoute",
				"responses": map[string]any{
					"201": map[string]any{"description": "Route created"},
				},
			},
		},
		"/gateway/api/routes/{id}": {
			"get": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "Get route by ID",
				"operationId": "getRoute",
				"parameters": []map[string]any{
					{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "Route details"},
				},
			},
			"put": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "Update route",
				"operationId": "updateRoute",
				"parameters": []map[string]any{
					{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "Route updated"},
				},
			},
			"delete": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "Delete route",
				"operationId": "deleteRoute",
				"parameters": []map[string]any{
					{"name": "id", "in": "path", "required": true, "schema": map[string]any{"type": "string"}},
				},
				"responses": map[string]any{
					"200": map[string]any{"description": "Route deleted"},
				},
			},
		},
		"/gateway/api/upstreams": {
			"get": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "List all upstream targets",
				"operationId": "listUpstreams",
				"responses": map[string]any{
					"200": map[string]any{"description": "List of upstream targets"},
				},
			},
		},
		"/gateway/api/stats": {
			"get": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "Get gateway statistics",
				"operationId": "getStats",
				"responses": map[string]any{
					"200": map[string]any{"description": "Gateway statistics"},
				},
			},
		},
		"/gateway/api/discovery/services": {
			"get": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "List discovered services",
				"operationId": "listDiscoveredServices",
				"responses": map[string]any{
					"200": map[string]any{"description": "List of discovered services"},
				},
			},
		},
		"/gateway/api/openapi/services": {
			"get": map[string]any{
				"tags":        []string{"gateway"},
				"summary":     "List services with OpenAPI specs",
				"operationId": "listOpenAPIServices",
				"responses": map[string]any{
					"200": map[string]any{"description": "Services with their OpenAPI spec status"},
				},
			},
		},
	}

	for path, pathItem := range gatewayRoutes {
		paths[path] = pathItem
	}
}

// tagOperations adds a tag to all operations in a path item.
func tagOperations(pathItem any, tag string) any {
	pathItemMap, ok := pathItem.(map[string]any)
	if !ok {
		return pathItem
	}

	methods := []string{"get", "put", "post", "delete", "options", "head", "patch", "trace"}
	for _, method := range methods {
		if op, ok := pathItemMap[method]; ok {
			if opMap, ok := op.(map[string]any); ok {
				existingTags, _ := opMap["tags"].([]any)
				// Add service tag if not already present
				hasTag := false
				for _, t := range existingTags {
					if t == tag {
						hasTag = true
						break
					}
				}
				if !hasTag {
					opMap["tags"] = append(existingTags, tag)
				}
			}
		}
	}

	return pathItemMap
}

// rewriteRefs rewrites $ref pointers to use namespaced schema names.
// Converts "#/components/schemas/Foo" -> "#/components/schemas/serviceName.Foo"
func rewriteRefs(obj any, serviceName string) {
	switch v := obj.(type) {
	case map[string]any:
		if ref, ok := v["$ref"].(string); ok {
			if strings.HasPrefix(ref, "#/components/schemas/") {
				schemaName := strings.TrimPrefix(ref, "#/components/schemas/")
				v["$ref"] = "#/components/schemas/" + serviceName + "." + schemaName
			}
		}

		for _, val := range v {
			rewriteRefs(val, serviceName)
		}

	case []any:
		for _, item := range v {
			rewriteRefs(item, serviceName)
		}
	}
}

// countPaths counts the number of paths in an OpenAPI spec.
func countPaths(spec map[string]any) int {
	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		return 0
	}

	return len(paths)
}

// isExcluded checks if a service name is in the exclusion list.
func (oa *OpenAPIAggregator) isExcluded(name string) bool {
	for _, excluded := range oa.config.ExcludeServices {
		if strings.EqualFold(excluded, name) {
			return true
		}
	}
	return false
}

// --- HTTP Handlers ---

// HandleMergedSpec serves the aggregated OpenAPI spec as JSON.
func (oa *OpenAPIAggregator) HandleMergedSpec(ctx forge.Context) error {
	specJSON := oa.MergedSpec()
	if specJSON == nil {
		return ctx.JSON(http.StatusServiceUnavailable, map[string]string{
			"error": "OpenAPI spec not yet available, try again shortly",
		})
	}

	ctx.Response().Header().Set("Content-Type", "application/json")
	ctx.Response().Header().Set("Cache-Control", "public, max-age=30")
	ctx.Response().WriteHeader(http.StatusOK)
	_, err := ctx.Response().Write(specJSON)

	return err
}

// HandleServiceSpec serves the OpenAPI spec for a specific service.
func (oa *OpenAPIAggregator) HandleServiceSpec(ctx forge.Context) error {
	serviceName := ctx.Param("service")
	if serviceName == "" {
		return ctx.JSON(http.StatusBadRequest, map[string]string{"error": "service name required"})
	}

	spec := oa.ServiceSpec(serviceName)
	if spec == nil {
		return ctx.JSON(http.StatusNotFound, map[string]string{"error": "service not found"})
	}

	if spec.Error != "" {
		return ctx.JSON(http.StatusBadGateway, map[string]any{
			"error":       spec.Error,
			"serviceName": spec.ServiceName,
			"specUrl":     spec.SpecURL,
		})
	}

	return ctx.JSON(http.StatusOK, spec.Spec)
}

// HandleServiceList returns a summary of all services with their OpenAPI spec status.
func (oa *OpenAPIAggregator) HandleServiceList(ctx forge.Context) error {
	specs := oa.ServiceSpecs()

	type serviceSummary struct {
		ServiceName string    `json:"serviceName"`
		Version     string    `json:"version"`
		SpecURL     string    `json:"specUrl"`
		Healthy     bool      `json:"healthy"`
		PathCount   int       `json:"pathCount"`
		Error       string    `json:"error,omitempty"`
		FetchedAt   time.Time `json:"fetchedAt"`
	}

	summaries := make([]serviceSummary, 0, len(specs))
	for _, spec := range specs {
		summaries = append(summaries, serviceSummary{
			ServiceName: spec.ServiceName,
			Version:     spec.Version,
			SpecURL:     spec.SpecURL,
			Healthy:     spec.Healthy,
			PathCount:   spec.PathCount,
			Error:       spec.Error,
			FetchedAt:   spec.FetchedAt,
		})
	}

	// Sort by name for deterministic output
	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].ServiceName < summaries[j].ServiceName
	})

	return ctx.JSON(http.StatusOK, map[string]any{
		"services":    summaries,
		"totalCount":  len(summaries),
		"lastRefresh": oa.LastRefresh(),
	})
}

// HandleRefresh triggers an immediate spec refresh.
func (oa *OpenAPIAggregator) HandleRefresh(ctx forge.Context) error {
	go oa.Refresh(ctx.Request().Context())

	return ctx.JSON(http.StatusOK, map[string]string{
		"status": "refresh initiated",
	})
}

// HandleSwaggerUI serves a Swagger UI page for the aggregated spec.
func (oa *OpenAPIAggregator) HandleSwaggerUI(ctx forge.Context) error {
	specPath := oa.config.Path

	html := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
  <title>%s - API Documentation</title>
  <meta charset="utf-8"/>
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" >
  <style>
    html { box-sizing: border-box; overflow: -moz-scrollbars-vertical; overflow-y: scroll; }
    *, *:before, *:after { box-sizing: inherit; }
    body { margin: 0; background: #fafafa; }
    .swagger-ui .topbar { display: none; }
  </style>
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-standalone-preset.js"></script>
  <script>
    window.onload = function() {
      SwaggerUIBundle({
        url: "%s",
        dom_id: '#swagger-ui',
        deepLinking: true,
        presets: [
          SwaggerUIBundle.presets.apis,
          SwaggerUIStandalonePreset
        ],
        plugins: [
          SwaggerUIBundle.plugins.DownloadUrl
        ],
        layout: "StandaloneLayout",
        defaultModelsExpandDepth: 1,
        defaultModelExpandDepth: 1,
        docExpansion: "list",
        filter: true,
        showExtensions: true,
        tagsSorter: "alpha",
        operationsSorter: "alpha"
      });
    }
  </script>
</body>
</html>`, oa.config.Title, specPath)

	ctx.Response().Header().Set("Content-Type", "text/html; charset=utf-8")
	ctx.Response().WriteHeader(http.StatusOK)
	_, err := ctx.Response().Write([]byte(html))

	return err
}
