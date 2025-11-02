package gateway

import (
	"context"
	"fmt"
	"sync"

	"github.com/xraph/forge/farp"
)

// Client is a reference implementation for API gateway integration
// It watches for service schema changes and provides conversion utilities
type Client struct {
	registry      farp.SchemaRegistry
	manifestCache map[string]*farp.SchemaManifest // key: instanceID
	schemaCache   map[string]interface{}          // key: hash
	mu            sync.RWMutex
}

// NewClient creates a new gateway client
func NewClient(registry farp.SchemaRegistry) *Client {
	return &Client{
		registry:      registry,
		manifestCache: make(map[string]*farp.SchemaManifest),
		schemaCache:   make(map[string]interface{}),
	}
}

// WatchServices watches for service registrations and schema updates
// onChange is called whenever services are added, updated, or removed
func (c *Client) WatchServices(ctx context.Context, serviceName string, onChange func([]ServiceRoute)) error {
	// Initial load
	manifests, err := c.registry.ListManifests(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to list initial manifests: %w", err)
	}

	// Convert initial manifests to routes
	routes := c.ConvertToRoutes(manifests)
	onChange(routes)

	// Watch for changes
	return c.registry.WatchManifests(ctx, serviceName, func(event *farp.ManifestEvent) {
		// Update manifest cache
		c.mu.Lock()
		switch event.Type {
		case farp.EventTypeAdded, farp.EventTypeUpdated:
			c.manifestCache[event.Manifest.InstanceID] = event.Manifest
		case farp.EventTypeRemoved:
			delete(c.manifestCache, event.Manifest.InstanceID)
		}

		// Get all cached manifests
		manifests := make([]*farp.SchemaManifest, 0, len(c.manifestCache))
		for _, m := range c.manifestCache {
			manifests = append(manifests, m)
		}
		c.mu.Unlock()

		// Convert to routes and notify
		routes := c.ConvertToRoutes(manifests)
		onChange(routes)
	})
}

// ConvertToRoutes converts service manifests to gateway routes
// This is a reference implementation - actual gateways should customize this
func (c *Client) ConvertToRoutes(manifests []*farp.SchemaManifest) []ServiceRoute {
	var routes []ServiceRoute

	for _, manifest := range manifests {
		// Fetch schemas for this manifest
		for _, schemaDesc := range manifest.Schemas {
			var schema interface{}
			var err error

			// Check cache first
			if cached, ok := c.getSchemaFromCache(schemaDesc.Hash); ok {
				schema = cached
			} else {
				// Fetch schema based on location type
				schema, err = c.fetchSchema(context.Background(), &schemaDesc)
				if err != nil {
					// Log error and skip
					continue
				}

				// Cache the schema
				c.cacheSchema(schemaDesc.Hash, schema)
			}

			// Convert schema to routes based on type
			switch schemaDesc.Type {
			case farp.SchemaTypeOpenAPI:
				routes = append(routes, c.convertOpenAPIToRoutes(manifest, schema)...)
			case farp.SchemaTypeAsyncAPI:
				routes = append(routes, c.convertAsyncAPIToRoutes(manifest, schema)...)
			case farp.SchemaTypeGraphQL:
				routes = append(routes, c.convertGraphQLToRoutes(manifest, schema)...)
			}
		}
	}

	return routes
}

// ServiceRoute represents a route configuration for the gateway
type ServiceRoute struct {
	// Path is the route path pattern
	Path string

	// Methods are HTTP methods for this route (e.g., ["GET", "POST"])
	Methods []string

	// TargetURL is the backend service URL
	TargetURL string

	// HealthURL is the health check URL
	HealthURL string

	// Middleware are middleware names to apply
	Middleware []string

	// Metadata contains additional route information
	Metadata map[string]interface{}

	// ServiceName is the name of the backend service
	ServiceName string

	// ServiceVersion is the version of the backend service
	ServiceVersion string
}

// fetchSchema fetches a schema based on its location
func (c *Client) fetchSchema(ctx context.Context, descriptor *farp.SchemaDescriptor) (interface{}, error) {
	switch descriptor.Location.Type {
	case farp.LocationTypeInline:
		return descriptor.InlineSchema, nil

	case farp.LocationTypeRegistry:
		return c.registry.FetchSchema(ctx, descriptor.Location.RegistryPath)

	case farp.LocationTypeHTTP:
		// TODO: Implement HTTP fetch
		// This would use an HTTP client to fetch from descriptor.Location.URL
		return nil, fmt.Errorf("HTTP schema fetch not yet implemented")

	default:
		return nil, fmt.Errorf("%w: %s", farp.ErrInvalidLocation, descriptor.Location.Type)
	}
}

// convertOpenAPIToRoutes converts an OpenAPI schema to gateway routes
func (c *Client) convertOpenAPIToRoutes(manifest *farp.SchemaManifest, schema interface{}) []ServiceRoute {
	var routes []ServiceRoute

	// Parse OpenAPI schema
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return routes
	}

	paths, ok := schemaMap["paths"].(map[string]interface{})
	if !ok {
		return routes
	}

	// Base URL for the service
	baseURL := fmt.Sprintf("http://%s:%d", manifest.ServiceName, 8080) // TODO: Get from manifest

	// Convert each path to a route
	for path, pathItem := range paths {
		pathItemMap, ok := pathItem.(map[string]interface{})
		if !ok {
			continue
		}

		// Get methods for this path
		methods := []string{}
		for method := range pathItemMap {
			switch method {
			case "get", "post", "put", "delete", "patch", "options", "head":
				methods = append(methods, method)
			}
		}

		if len(methods) > 0 {
			routes = append(routes, ServiceRoute{
				Path:           path,
				Methods:        methods,
				TargetURL:      baseURL + path,
				HealthURL:      baseURL + manifest.Endpoints.Health,
				ServiceName:    manifest.ServiceName,
				ServiceVersion: manifest.ServiceVersion,
				Metadata: map[string]interface{}{
					"schema_type": "openapi",
				},
			})
		}
	}

	return routes
}

// convertAsyncAPIToRoutes converts an AsyncAPI schema to gateway routes (WebSocket, SSE)
func (c *Client) convertAsyncAPIToRoutes(manifest *farp.SchemaManifest, schema interface{}) []ServiceRoute {
	var routes []ServiceRoute

	// Parse AsyncAPI schema
	schemaMap, ok := schema.(map[string]interface{})
	if !ok {
		return routes
	}

	channels, ok := schemaMap["channels"].(map[string]interface{})
	if !ok {
		return routes
	}

	// Base URL for the service
	baseURL := fmt.Sprintf("http://%s:%d", manifest.ServiceName, 8080)

	// Convert each channel to a route
	for channelPath := range channels {
		routes = append(routes, ServiceRoute{
			Path:           channelPath,
			Methods:        []string{"WEBSOCKET"}, // Special method for WebSocket
			TargetURL:      baseURL + channelPath,
			HealthURL:      baseURL + manifest.Endpoints.Health,
			ServiceName:    manifest.ServiceName,
			ServiceVersion: manifest.ServiceVersion,
			Metadata: map[string]interface{}{
				"schema_type": "asyncapi",
				"protocol":    "websocket",
			},
		})
	}

	return routes
}

// convertGraphQLToRoutes converts a GraphQL schema to a gateway route
func (c *Client) convertGraphQLToRoutes(manifest *farp.SchemaManifest, schema interface{}) []ServiceRoute {
	var routes []ServiceRoute

	// GraphQL typically has a single endpoint
	baseURL := fmt.Sprintf("http://%s:%d", manifest.ServiceName, 8080)
	graphqlPath := manifest.Endpoints.GraphQL
	if graphqlPath == "" {
		graphqlPath = "/graphql"
	}

	routes = append(routes, ServiceRoute{
		Path:           graphqlPath,
		Methods:        []string{"POST", "GET"},
		TargetURL:      baseURL + graphqlPath,
		HealthURL:      baseURL + manifest.Endpoints.Health,
		ServiceName:    manifest.ServiceName,
		ServiceVersion: manifest.ServiceVersion,
		Metadata: map[string]interface{}{
			"schema_type": "graphql",
		},
	})

	return routes
}

// getSchemaFromCache retrieves a cached schema by hash
func (c *Client) getSchemaFromCache(hash string) (interface{}, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	schema, ok := c.schemaCache[hash]
	return schema, ok
}

// cacheSchema stores a schema in cache
func (c *Client) cacheSchema(hash string, schema interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.schemaCache[hash] = schema
}

// ClearCache clears the schema cache
func (c *Client) ClearCache() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.schemaCache = make(map[string]interface{})
}

// GetManifest retrieves a cached manifest by instance ID
func (c *Client) GetManifest(instanceID string) (*farp.SchemaManifest, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	manifest, ok := c.manifestCache[instanceID]
	return manifest, ok
}

