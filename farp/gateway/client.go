package gateway

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/xraph/farp"
)

// DefaultSchemaCacheSize is the number of verified schemas a Client keeps
// when no explicit size is configured.
const DefaultSchemaCacheSize = 1024

// ErrSchemaHashMismatch is returned when a schema's content does not hash to
// the value its descriptor claims.
var ErrSchemaHashMismatch = errors.New("farp gateway: schema content does not match descriptor hash")

// Client is a reference implementation for API gateway integration
// It watches for service schema changes and provides conversion utilities.
type Client struct {
	registry      farp.SchemaRegistry
	manifestCache map[string]*farp.SchemaManifest // key: instanceID
	schemaCache   *schemaLRU
	logger        *slog.Logger
	mu            sync.RWMutex
}

// ClientOption configures a Client.
type ClientOption func(*Client)

// WithSchemaCacheSize bounds the number of schemas kept in the client's cache.
// Values below 1 fall back to DefaultSchemaCacheSize.
func WithSchemaCacheSize(n int) ClientOption {
	return func(c *Client) {
		c.schemaCache = newSchemaLRU(n)
	}
}

// WithLogger makes the client log schemas it skips because they could not be
// fetched or their content did not match the descriptor hash. Without it the
// client skips them silently.
func WithLogger(logger *slog.Logger) ClientOption {
	return func(c *Client) {
		c.logger = logger
	}
}

// NewClient creates a new gateway client.
func NewClient(registry farp.SchemaRegistry, opts ...ClientOption) *Client {
	c := &Client{
		registry:      registry,
		manifestCache: make(map[string]*farp.SchemaManifest),
		schemaCache:   newSchemaLRU(DefaultSchemaCacheSize),
	}

	for _, opt := range opts {
		opt(c)
	}

	return c
}

// WatchServices watches for service registrations and schema updates
// onChange is called whenever services are added, updated, or removed.
func (c *Client) WatchServices(ctx context.Context, serviceName string, onChange func([]ServiceRoute)) error {
	// Initial load
	manifests, err := c.registry.ListManifests(ctx, serviceName)
	if err != nil {
		return fmt.Errorf("failed to list initial manifests: %w", err)
	}

	// Seed the manifest cache so the first watch event is applied on top of
	// the full initial set rather than replacing it.
	c.mu.Lock()

	for _, m := range manifests {
		if m != nil {
			c.manifestCache[m.InstanceID] = m
		}
	}

	c.mu.Unlock()

	// Convert initial manifests to routes
	routes := c.ConvertToRoutes(manifests)
	onChange(routes)

	// Watch for changes
	return c.registry.WatchManifests(ctx, serviceName, func(event *farp.ManifestEvent) {
		// A registry may deliver nil once it is closed. There is nothing to
		// apply in that case, and dereferencing it would panic.
		if event == nil || event.Manifest == nil {
			return
		}

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
// This is a reference implementation - actual gateways should customize this.
func (c *Client) ConvertToRoutes(manifests []*farp.SchemaManifest) []ServiceRoute {
	var routes []ServiceRoute

	for _, manifest := range manifests {
		if manifest == nil {
			continue
		}

		// Fetch schemas for this manifest
		for _, schemaDesc := range manifest.Schemas {
			schema, err := c.resolveSchema(context.Background(), manifest.InstanceID, &schemaDesc)
			if err != nil {
				// Unfetchable or unverifiable schemas produce no routes.
				if c.logger != nil {
					c.logger.Warn("farp gateway: skipping schema",
						slog.String("service", manifest.ServiceName),
						slog.String("instance_id", manifest.InstanceID),
						slog.String("schema_type", string(schemaDesc.Type)),
						slog.String("hash", schemaDesc.Hash),
						slog.Any("error", err),
					)
				}

				continue
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

// ServiceRoute represents a route configuration for the gateway.
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
	Metadata map[string]any

	// ServiceName is the name of the backend service
	ServiceName string

	// ServiceVersion is the version of the backend service
	ServiceVersion string
}

// resolveSchema returns the schema for a descriptor, from cache when this
// instance has already presented verified content for the same hash.
//
// The hash in a descriptor is supplied by the registrant, so it is never
// trusted on its own: fetched content is hashed again and rejected when it
// does not match. The cache is keyed by instance ID as well as hash, so one
// registrant can never populate the entry another registrant reads.
func (c *Client) resolveSchema(ctx context.Context, instanceID string, desc *farp.SchemaDescriptor) (any, error) {
	key := schemaCacheKey{instanceID: instanceID, hash: desc.Hash}

	if cached, ok := c.schemaCache.get(key); ok {
		return cached, nil
	}

	schema, err := c.fetchSchema(ctx, desc)
	if err != nil {
		return nil, err
	}

	if err := VerifySchemaHash(schema, desc.Hash); err != nil {
		return nil, err
	}

	c.schemaCache.put(key, schema)

	return schema, nil
}

// VerifySchemaHash recomputes the SHA-256 checksum of schema with
// farp.CalculateSchemaChecksum and compares it to expected.
func VerifySchemaHash(schema any, expected string) error {
	if expected == "" {
		return fmt.Errorf("%w: descriptor has no hash", ErrSchemaHashMismatch)
	}

	actual, err := farp.CalculateSchemaChecksum(schema)
	if err != nil {
		return fmt.Errorf("farp gateway: hash schema: %w", err)
	}

	if actual != expected {
		return fmt.Errorf("%w: descriptor claims %s, content hashes to %s", ErrSchemaHashMismatch, expected, actual)
	}

	return nil
}

// fetchSchema fetches a schema based on its location.
func (c *Client) fetchSchema(ctx context.Context, descriptor *farp.SchemaDescriptor) (any, error) {
	switch descriptor.Location.Type {
	case farp.LocationTypeInline:
		return descriptor.InlineSchema, nil

	case farp.LocationTypeRegistry:
		return c.registry.FetchSchema(ctx, descriptor.Location.RegistryPath)

	case farp.LocationTypeHTTP:
		// TODO: Implement HTTP fetch
		// This would use an HTTP client to fetch from descriptor.Location.URL
		return nil, errors.New("HTTP schema fetch not yet implemented")

	default:
		return nil, fmt.Errorf("%w: %s", farp.ErrInvalidLocation, descriptor.Location.Type)
	}
}

// convertOpenAPIToRoutes converts an OpenAPI schema to gateway routes.
func (c *Client) convertOpenAPIToRoutes(manifest *farp.SchemaManifest, schema any) []ServiceRoute {
	var routes []ServiceRoute

	// Parse OpenAPI schema
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return routes
	}

	paths, ok := schemaMap["paths"].(map[string]any)
	if !ok {
		return routes
	}

	// Base URL for the service
	baseURL := fmt.Sprintf("http://%s:%d", manifest.ServiceName, 8080) // TODO: Get from manifest

	// Convert each path to a route
	for path, pathItem := range paths {
		pathItemMap, ok := pathItem.(map[string]any)
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
				Metadata: map[string]any{
					"schema_type": "openapi",
				},
			})
		}
	}

	return routes
}

// convertAsyncAPIToRoutes converts an AsyncAPI schema to gateway routes (WebSocket, SSE).
func (c *Client) convertAsyncAPIToRoutes(manifest *farp.SchemaManifest, schema any) []ServiceRoute {
	var routes []ServiceRoute

	// Parse AsyncAPI schema
	schemaMap, ok := schema.(map[string]any)
	if !ok {
		return routes
	}

	channels, ok := schemaMap["channels"].(map[string]any)
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
			Metadata: map[string]any{
				"schema_type": "asyncapi",
				"protocol":    "websocket",
			},
		})
	}

	return routes
}

// convertGraphQLToRoutes converts a GraphQL schema to a gateway route.
func (c *Client) convertGraphQLToRoutes(manifest *farp.SchemaManifest, schema any) []ServiceRoute {
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
		Metadata: map[string]any{
			"schema_type": "graphql",
		},
	})

	return routes
}

// ClearCache clears the schema cache.
func (c *Client) ClearCache() {
	c.schemaCache.clear()
}

// GetManifest retrieves a cached manifest by instance ID.
func (c *Client) GetManifest(instanceID string) (*farp.SchemaManifest, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	manifest, ok := c.manifestCache[instanceID]

	return manifest, ok
}

type schemaCacheKey struct {
	instanceID string
	hash       string
}

type schemaCacheEntry struct {
	key    schemaCacheKey
	schema any
}

// schemaLRU is a size-bounded least-recently-used cache of verified schemas.
type schemaLRU struct {
	mu      sync.Mutex
	max     int
	order   *list.List
	entries map[schemaCacheKey]*list.Element
}

func newSchemaLRU(maxEntries int) *schemaLRU {
	if maxEntries < 1 {
		maxEntries = DefaultSchemaCacheSize
	}

	return &schemaLRU{
		max:     maxEntries,
		order:   list.New(),
		entries: make(map[schemaCacheKey]*list.Element),
	}
}

func (l *schemaLRU) get(key schemaCacheKey) (any, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	el, ok := l.entries[key]
	if !ok {
		return nil, false
	}

	l.order.MoveToFront(el)

	return el.Value.(*schemaCacheEntry).schema, true
}

func (l *schemaLRU) put(key schemaCacheKey, schema any) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if el, ok := l.entries[key]; ok {
		el.Value.(*schemaCacheEntry).schema = schema
		l.order.MoveToFront(el)

		return
	}

	l.entries[key] = l.order.PushFront(&schemaCacheEntry{key: key, schema: schema})

	for l.order.Len() > l.max {
		oldest := l.order.Back()
		l.order.Remove(oldest)
		delete(l.entries, oldest.Value.(*schemaCacheEntry).key)
	}
}

func (l *schemaLRU) len() int {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.order.Len()
}

func (l *schemaLRU) clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.order.Init()
	l.entries = make(map[schemaCacheKey]*list.Element)
}
