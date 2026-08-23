package client

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/xraph/forge/internal/shared"
	"gopkg.in/yaml.v3"
)

// SpecParser parses OpenAPI and AsyncAPI specification files.
type SpecParser struct{}

// NewSpecParser creates a new spec parser.
func NewSpecParser() *SpecParser {
	return &SpecParser{}
}

// ParseFile parses a specification file and resolves entity field edges.
// This is the single-source path and its behaviour is unchanged.
func (p *SpecParser) ParseFile(ctx context.Context, filePath string) (*APISpec, error) {
	spec, err := p.ParseFileUnresolved(ctx, filePath)
	if err != nil {
		return nil, err
	}

	// Entity-to-entity field edges, once every entity in the document is
	// known. Same call, same point in construction, as
	// Introspector.Introspect makes for a live router -- one function, so the
	// two paths cannot drift.
	resolveEntityFields(spec)

	// No YAML-specific degradation warning is emitted here any more. It used to
	// be, because yaml.v3 does not consult MarshalJSON/UnmarshalJSON and the
	// extension-carrying types in internal/shared implemented only those, so
	// every x-forge-* extension in a YAML spec was silently dropped. Those types
	// now implement MarshalYAML/UnmarshalYAML as well, so a YAML source carries
	// entity identity, cache tags and stream bindings exactly as a JSON one
	// does; see internal/client/spec_parser_yaml_meta_test.go, which drives this
	// function against real .yaml/.yml files.
	return spec, nil
}

// ParseFileUnresolved parses a specification file without resolving entity
// field edges.
//
// resolveEntityFields is idempotent -- each call replaces an entity's Fields
// and rebuilds spec.RoutingTypes from scratch rather than merging into what
// was there, so resolving once per document and again after MergeSpecs would
// still land on the correct answer. This split exists anyway, for two reasons
// that are about the work, not its correctness:
//
//   - Resolving per document computes edges over a schema set that a merge is
//     about to replace with the union of every document's schemas, so any
//     edge that depends on a type only a DIFFERENT document defines is thrown
//     away and then recomputed correctly on the next call. That work is pure
//     waste when a merge is coming.
//   - Resolution wants to run exactly where the complete schema set is known.
//     For a single-source parse that is ParseFile's own return; for a merge
//     it is only true after MergeSpecs has combined every source. Giving the
//     caller ParseFileUnresolved lets it defer resolution to that point
//     instead of performing it once per source and once more for real.
//
// The caller is responsible for calling resolveEntityFields, directly or via
// ParseFile.
func (p *SpecParser) ParseFileUnresolved(ctx context.Context, filePath string) (*APISpec, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read spec file: %w", err)
	}

	// Determine file type by extension
	ext := strings.ToLower(filepath.Ext(filePath))
	isYAML := ext == ".yaml" || ext == ".yml"

	// Try to determine spec type by content
	specType, err := p.detectSpecType(data, isYAML)
	if err != nil {
		return nil, fmt.Errorf("detect spec type: %w", err)
	}

	var spec *APISpec

	switch specType {
	case "openapi":
		spec, err = p.parseOpenAPI(data, isYAML)
		if spec != nil {
			spec.Kind = SourceOpenAPI
		}
	case "asyncapi":
		spec, err = p.parseAsyncAPI(data, isYAML)
		if spec != nil {
			spec.Kind = SourceAsyncAPI
		}
	default:
		return nil, fmt.Errorf("unknown spec type: %s", specType)
	}

	if err != nil {
		return nil, err
	}

	return spec, nil
}

// detectSpecType detects whether the spec is OpenAPI or AsyncAPI.
func (p *SpecParser) detectSpecType(data []byte, isYAML bool) (string, error) {
	var raw map[string]any

	if isYAML {
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return "", err
		}
	} else {
		if err := json.Unmarshal(data, &raw); err != nil {
			return "", err
		}
	}

	// Check for OpenAPI version field
	if openapi, ok := raw["openapi"].(string); ok && openapi != "" {
		return "openapi", nil
	}

	// Check for AsyncAPI version field
	if asyncapi, ok := raw["asyncapi"].(string); ok && asyncapi != "" {
		return "asyncapi", nil
	}

	return "", errors.New("spec does not contain 'openapi' or 'asyncapi' version field")
}

// parseOpenAPI parses an OpenAPI specification.
func (p *SpecParser) parseOpenAPI(data []byte, isYAML bool) (*APISpec, error) {
	var openAPISpec shared.OpenAPISpec

	if isYAML {
		//nolint:musttag // OpenAPISpec has yaml tags defined in shared package
		if err := yaml.Unmarshal(data, &openAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal OpenAPI YAML: %w", err)
		}
	} else {
		//nolint:musttag // OpenAPISpec has json tags defined in shared package
		if err := json.Unmarshal(data, &openAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal OpenAPI JSON: %w", err)
		}
	}

	// Convert to IR
	spec := &APISpec{
		Schemas:  make(map[string]*Schema),
		Security: []SecurityScheme{},
	}

	// Extract info
	spec.Info = APIInfo{
		Title:       openAPISpec.Info.Title,
		Version:     openAPISpec.Info.Version,
		Description: openAPISpec.Info.Description,
	}

	if openAPISpec.Info.Contact != nil {
		spec.Info.Contact = &Contact{
			Name:  openAPISpec.Info.Contact.Name,
			URL:   openAPISpec.Info.Contact.URL,
			Email: openAPISpec.Info.Contact.Email,
		}
	}

	if openAPISpec.Info.License != nil {
		spec.Info.License = &License{
			Name: openAPISpec.Info.License.Name,
			URL:  openAPISpec.Info.License.URL,
		}
	}

	// Extract servers
	for _, srv := range openAPISpec.Servers {
		server := Server{
			URL:         srv.URL,
			Description: srv.Description,
			Variables:   make(map[string]ServerVariable),
		}
		for k, v := range srv.Variables {
			server.Variables[k] = ServerVariable{
				Default:     v.Default,
				Description: v.Description,
				Enum:        v.Enum,
			}
		}

		spec.Servers = append(spec.Servers, server)
	}

	// Extract security schemes
	if openAPISpec.Components != nil && openAPISpec.Components.SecuritySchemes != nil {
		for name, scheme := range openAPISpec.Components.SecuritySchemes {
			secScheme := SecurityScheme{
				Key:              name,
				ParamName:        scheme.Name,
				Type:             scheme.Type,
				Description:      scheme.Description,
				In:               scheme.In,
				Scheme:           scheme.Scheme,
				BearerFormat:     scheme.BearerFormat,
				OpenIDConnectURL: scheme.OpenIdConnectUrl,
			}

			if scheme.Flows != nil {
				secScheme.Flows = convertOAuthFlows(scheme.Flows)
			}

			spec.Security = append(spec.Security, secScheme)
		}

		// Sorted because the range above is over a map. The generated
		// AuthConfig's field order is this order, and output that moves
		// between runs of the same input is a diff in every repository that
		// regenerates. `auth.go` makes the same argument one level down, for
		// endpoint security, and calls the sort load-bearing rather than
		// cosmetic.
		sort.Slice(spec.Security, func(i, j int) bool {
			return spec.Security[i].Key < spec.Security[j].Key
		})
	}

	// Extract schemas
	if openAPISpec.Components != nil && openAPISpec.Components.Schemas != nil {
		for name, schema := range openAPISpec.Components.Schemas {
			spec.Schemas[name] = convertSchema(schema)
		}
	}

	// Extract tags
	for _, tag := range openAPISpec.Tags {
		spec.Tags = append(spec.Tags, Tag{
			Name:        tag.Name,
			Description: tag.Description,
		})
	}

	// Extract endpoints.
	//
	// Sorted paths, fixed method order: see spec_order.go. An unsorted walk
	// reorders spec.Endpoints between two parses of the same file, and every
	// generated file that iterates endpoints reorders with it.
	for _, path := range sortedPathKeys(openAPISpec.Paths) {
		pathItem := openAPISpec.Paths[path]
		if pathItem == nil {
			continue
		}

		for _, mo := range orderedPathOps(pathItem) {
			endpoint := convertOperation(spec, mo.Method, path, mo.Op)
			spec.Endpoints = append(spec.Endpoints, endpoint)
		}
	}

	return spec, nil
}

// parseAsyncAPI parses an AsyncAPI specification.
func (p *SpecParser) parseAsyncAPI(data []byte, isYAML bool) (*APISpec, error) {
	var asyncAPISpec shared.AsyncAPISpec

	if isYAML {
		//nolint:musttag // AsyncAPISpec has yaml tags defined in shared package
		if err := yaml.Unmarshal(data, &asyncAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal AsyncAPI YAML: %w", err)
		}
	} else {
		if err := json.Unmarshal(data, &asyncAPISpec); err != nil {
			return nil, fmt.Errorf("unmarshal AsyncAPI JSON: %w", err)
		}
	}

	// Convert to IR
	spec := &APISpec{
		Schemas:  make(map[string]*Schema),
		Security: []SecurityScheme{},
	}

	// Extract info
	spec.Info = APIInfo{
		Title:       asyncAPISpec.Info.Title,
		Version:     asyncAPISpec.Info.Version,
		Description: asyncAPISpec.Info.Description,
	}

	if asyncAPISpec.Info.Contact != nil {
		spec.Info.Contact = &Contact{
			Name:  asyncAPISpec.Info.Contact.Name,
			URL:   asyncAPISpec.Info.Contact.URL,
			Email: asyncAPISpec.Info.Contact.Email,
		}
	}

	if asyncAPISpec.Info.License != nil {
		spec.Info.License = &License{
			Name: asyncAPISpec.Info.License.Name,
			URL:  asyncAPISpec.Info.License.URL,
		}
	}

	// Extract schemas from components
	if asyncAPISpec.Components != nil && asyncAPISpec.Components.Schemas != nil {
		for name, schema := range asyncAPISpec.Components.Schemas {
			spec.Schemas[name] = convertSchema(schema)
		}
	}

	// Extract servers, in sorted server-name order. AsyncAPI keys servers by
	// name rather than listing them, and spec.Servers is rendered as-is into
	// the README's Servers section, so ranging the map reshuffled that section
	// between runs on any document declaring more than one server.
	for _, name := range sortedStringKeys(asyncAPISpec.Servers) {
		srv := asyncAPISpec.Servers[name]
		server := Server{
			URL:         fmt.Sprintf("%s://%s%s", srv.Protocol, srv.Host, srv.Pathname),
			Description: srv.Description,
			Variables:   make(map[string]ServerVariable),
		}
		spec.Servers = append(spec.Servers, server)
	}

	// Extract operations and channels
	wsEndpoints := make(map[string]*WebSocketEndpoint)
	sseEndpoints := make(map[string]*SSEEndpoint)

	// Sorted operation ids, for the same reason paths are sorted above: this
	// loop decides both the order streaming endpoints reach the IR and, where
	// several operations share one channel, which operation converts the
	// channel and which merely merges into it.
	for _, opID := range sortedStringKeys(asyncAPISpec.Operations) {
		operation := asyncAPISpec.Operations[opID]
		if operation == nil || operation.Channel == nil {
			continue
		}

		channelRef := operation.Channel.Ref
		if channelRef == "" {
			continue
		}

		channelName := strings.TrimPrefix(channelRef, "#/channels/")

		channel := asyncAPISpec.Channels[channelName]
		if channel == nil {
			continue
		}

		// Determine if WebSocket or SSE
		isWebSocket := detectWebSocketChannel(&asyncAPISpec, channel)

		if isWebSocket {
			// Use channel name as key to merge operations on same channel
			if wsEndpoints[channelName] == nil {
				ws := convertWebSocketChannel(spec, opID, channel, operation)
				wsEndpoints[channelName] = &ws
			} else {
				// Merge with existing endpoint
				existing := wsEndpoints[channelName]
				if operation.Action == "send" && existing.SendSchema == nil {
					existing.SendSchema = convertSchemaFromChannel(channel, operation)
				} else if operation.Action == "receive" && existing.ReceiveSchema == nil {
					existing.ReceiveSchema = convertSchemaFromChannel(channel, operation)
				}
			}
		} else {
			// Use channel name as key to merge operations on same channel
			if sseEndpoints[channelName] == nil {
				sse := convertSSEChannel(spec, opID, channel, operation)
				sseEndpoints[channelName] = &sse
			} else {
				// Merge event schemas
				existing := sseEndpoints[channelName]

				for msgName, msg := range channel.Messages {
					if msg.Payload != nil {
						existing.EventSchemas[msgName] = convertSchema(msg.Payload)
					}
				}
			}
		}
	}

	// Add merged endpoints to spec, keyed by channel name in sorted order --
	// these are maps, and appending in iteration order would reshuffle
	// websocket.ts and sse.ts between runs.
	for _, name := range sortedStringKeys(wsEndpoints) {
		spec.WebSockets = append(spec.WebSockets, *wsEndpoints[name])
	}

	for _, name := range sortedStringKeys(sseEndpoints) {
		spec.SSEs = append(spec.SSEs, *sseEndpoints[name])
	}

	return spec, nil
}

// Helper conversion functions

func convertSchemaFromChannel(channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) *Schema {
	// Lowest message name wins rather than whichever the map happened to hand
	// over first. A channel carrying several messages otherwise contributed a
	// different send/receive schema on each run, and that schema is emitted as
	// a named type in the generated client.
	for _, name := range sortedStringKeys(channel.Messages) {
		if msg := channel.Messages[name]; msg.Payload != nil {
			return convertSchema(msg.Payload)
		}
	}

	return nil
}

func convertOperation(spec *APISpec, method, path string, op *shared.Operation) Endpoint {
	endpoint := Endpoint{
		Method:      method,
		Path:        path,
		Summary:     op.Summary,
		Description: op.Description,
		Tags:        op.Tags,
		OperationID: op.OperationID,
		Deprecated:  op.Deprecated,
		Responses:   make(map[int]*Response),
		Metadata:    make(map[string]any),
	}

	// Extract parameters
	for _, param := range op.Parameters {
		p := Parameter{
			Name:        param.Name,
			In:          param.In,
			Description: param.Description,
			Required:    param.Required,
			Deprecated:  param.Deprecated,
			Schema:      convertSchema(param.Schema),
			Example:     param.Example,
		}

		switch param.In {
		case "path":
			endpoint.PathParams = append(endpoint.PathParams, p)
		case "query":
			endpoint.QueryParams = append(endpoint.QueryParams, p)
		case "header":
			endpoint.HeaderParams = append(endpoint.HeaderParams, p)
		case "cookie":
			endpoint.CookieParams = append(endpoint.CookieParams, p)
		default:
			// Reported rather than dropped. A parameter that vanishes here
			// produces a client that compiles, runs, and quietly omits
			// something the API declared -- which is exactly how `in: cookie`
			// went missing until somebody tried to use session auth.
			spec.Warnings = append(spec.Warnings, fmt.Sprintf(
				"parameter %q on %s %s declares an unknown location %q and was skipped",
				param.Name, endpoint.Method, endpoint.Path, param.In,
			))
		}
	}

	// Extract request body
	if op.RequestBody != nil {
		endpoint.RequestBody = &RequestBody{
			Description: op.RequestBody.Description,
			Required:    op.RequestBody.Required,
			Content:     make(map[string]*MediaType),
		}

		for contentType, media := range op.RequestBody.Content {
			endpoint.RequestBody.Content[contentType] = &MediaType{
				Schema:   convertSchema(media.Schema),
				Example:  media.Example,
				Examples: convertExamples(media.Examples),
			}
		}
	}

	// Extract responses.
	//
	// Status codes arrive as strings: an exact code ("200"), the catch-all
	// ("default"), or a class wildcard ("2XX"). Exact codes are applied before
	// wildcards, so a spec declaring both 200 and 2XX keeps the specific one —
	// map iteration order is random, and a single pass would let whichever
	// happened to come last win.
	//
	// A key that is none of these is skipped rather than filed under
	// DefaultError. Treating an unparseable status as the default is how a
	// typo silently becomes an endpoint's error shape, which is worse than
	// dropping it: the generators read DefaultError to type failures.
	type pendingResponse struct {
		code         int
		fromWildcard bool
		response     *Response
	}

	pending := make([]pendingResponse, 0, len(op.Responses))

	for statusCode, resp := range op.Responses {
		response := &Response{
			Description: resp.Description,
			Content:     make(map[string]*MediaType),
			Headers:     make(map[string]*Parameter),
		}

		for contentType, media := range resp.Content {
			response.Content[contentType] = &MediaType{
				Schema:   convertSchema(media.Schema),
				Example:  media.Example,
				Examples: convertExamples(media.Examples),
			}
		}

		for headerName, header := range resp.Headers {
			response.Headers[headerName] = &Parameter{
				Name:        headerName,
				In:          "header",
				Description: header.Description,
				Required:    header.Required,
				Schema:      convertSchema(header.Schema),
			}
		}

		if statusCode == "default" {
			endpoint.DefaultError = response

			continue
		}

		code, wildcard, ok := parseStatusCode(statusCode)
		if !ok {
			continue
		}

		pending = append(pending, pendingResponse{
			code:         code,
			fromWildcard: wildcard,
			response:     response,
		})
	}

	sort.SliceStable(pending, func(i, j int) bool {
		return !pending[i].fromWildcard && pending[j].fromWildcard
	})

	for _, p := range pending {
		if p.fromWildcard {
			if _, exists := endpoint.Responses[p.code]; exists {
				continue
			}
		}

		endpoint.Responses[p.code] = p.response
	}

	// Extract security
	// Scheme names within one requirement object are walked in sorted order.
	// The order survives into Endpoint.Security, and capabilityAlternatives
	// preserves it when it builds the scope alternatives that capabilities.ts
	// and capabilities.go emit verbatim -- so ranging the map directly made
	// those two files differ between runs whenever a single requirement object
	// named more than one scheme.
	for _, secReq := range op.Security {
		for _, name := range sortedStringKeys(secReq) {
			endpoint.Security = append(endpoint.Security, SecurityRequirement{
				SchemeName: name,
				Scopes:     secReq[name],
			})
		}
	}

	resolveEndpointCacheMeta(spec, &endpoint, op.Extensions)
	endpoint.Authorization = resolveEndpointAuthz(op.Extensions)

	return endpoint
}

// parseStatusCode interprets an OpenAPI response key.
//
// Returns the numeric code, whether it came from a class wildcard, and whether
// it was understood at all. Wildcards ("2XX") normalise to the base of their
// class, which is what the generators' 2xx-range scans already look for; the
// caller keeps an exact code in preference to a wildcard that lands on it.
//
// Codes outside 100-599 are rejected. A response declared as "999" is a
// mistake in the spec, and admitting it would put a body under a status no
// transport will ever produce.
func parseStatusCode(key string) (code int, wildcard bool, ok bool) {
	if n, err := strconv.Atoi(key); err == nil {
		if n < 100 || n > 599 {
			return 0, false, false
		}

		return n, false, true
	}

	if len(key) == 3 && (key[1] == 'X' || key[1] == 'x') && (key[2] == 'X' || key[2] == 'x') {
		if key[0] >= '1' && key[0] <= '5' {
			return int(key[0]-'0') * 100, true, true
		}
	}

	return 0, false, false
}

func convertSchema(s *shared.Schema) *Schema {
	if s == nil {
		return nil
	}

	schema := &Schema{
		Type:        s.Type,
		Format:      s.Format,
		Description: s.Description,
		Required:    s.Required,
		Enum:        s.Enum,
		Default:     s.Default,
		Example:     s.Example,
		Nullable:    s.Nullable,
		ReadOnly:    s.ReadOnly,
		WriteOnly:   s.WriteOnly,
		Pattern:     s.Pattern,
		Ref:         s.Ref,
		Extensions:  s.Extensions,
	}

	schema.AdditionalProperties = normalizeAdditionalProperties(s.AdditionalProperties)

	if s.MinLength > 0 {
		minLen := s.MinLength
		schema.MinLength = &minLen
	}

	if s.MaxLength > 0 {
		maxLen := s.MaxLength
		schema.MaxLength = &maxLen
	}

	if s.Minimum != 0 {
		minVal := s.Minimum
		schema.Minimum = &minVal
	}

	if s.Maximum != 0 {
		maxVal := s.Maximum
		schema.Maximum = &maxVal
	}

	if len(s.Properties) > 0 {
		schema.Properties = make(map[string]*Schema)
		for k, v := range s.Properties {
			schema.Properties[k] = convertSchema(v)
		}
	}

	if s.Items != nil {
		schema.Items = convertSchema(s.Items)
	}

	if len(s.OneOf) > 0 {
		for idx := range s.OneOf {
			schema.OneOf = append(schema.OneOf, convertSchema(&s.OneOf[idx]))
		}
	}

	if len(s.AnyOf) > 0 {
		for idx := range s.AnyOf {
			schema.AnyOf = append(schema.AnyOf, convertSchema(&s.AnyOf[idx]))
		}
	}

	if len(s.AllOf) > 0 {
		for idx := range s.AllOf {
			schema.AllOf = append(schema.AllOf, convertSchema(&s.AllOf[idx]))
		}
	}

	if s.Discriminator != nil {
		schema.Discriminator = &Discriminator{
			PropertyName: s.Discriminator.PropertyName,
			Mapping:      s.Discriminator.Mapping,
		}
	}

	return schema
}

// normalizeAdditionalProperties converts the raw decoder output for
// additionalProperties into the shape the rest of the pipeline (and
// ultimately the TypeScript generator's additionalPropsSchema) expects: a
// bool, or a *Schema.
//
// JSON Schema allows additionalProperties to be either a bool or a schema
// object, but shared.Schema.AdditionalProperties is typed `any`, and neither
// encoding/json nor yaml.v3 knows to decode an object-valued field typed
// `any` into *shared.Schema — both decode it generically instead: a JSON/YAML
// boolean becomes a Go bool (already the shape this IR wants, so it passes
// through unchanged), while a JSON/YAML object becomes map[string]any (JSON)
// or map[string]interface{} (YAML, once the field carries an explicit yaml
// tag — see the tag comment on shared.Schema.AdditionalProperties). Both are
// the same underlying Go type (map[string]interface{} *is* map[string]any),
// confirmed by exercising this exact field through both decoders.
//
// Rather than hand-walking that map a second time, the raw value is
// re-marshalled to JSON and unmarshalled into a shared.Schema, then run
// through convertSchema itself. That reuses the existing, already-correct
// conversion for every nested field (items, properties, $ref, format,
// enums, ...) for free and stays correct as shared.Schema grows, instead of
// drifting out of sync with a second, parallel conversion.
//
// A malformed document whose additionalProperties is neither a bool nor an
// object — invalid per the JSON Schema spec, but nothing stops a
// hand-written file from doing it — fails the round trip. That failure is
// not swallowed silently: it's logged, and the original raw value is
// returned unchanged, so behaviour for that one field is exactly what it
// was before this function existed (the generator's additionalPropsSchema
// falls through its default case for anything that isn't nil/bool/*Schema).
// The whole parse is deliberately not failed for this: a spec that
// previously parsed successfully (if imperfectly, on this one field) must
// keep parsing successfully — degrading gracefully on a single malformed
// keyword is preferable to turning it into a hard failure for the entire
// file.
func normalizeAdditionalProperties(v any) any {
	switch v.(type) {
	case nil, bool:
		return v
	}

	raw, err := json.Marshal(v)
	if err != nil {
		log.Printf("client: additionalProperties: marshal %T: %v", v, err)
		return v
	}

	var nested shared.Schema
	if err := json.Unmarshal(raw, &nested); err != nil {
		log.Printf("client: additionalProperties: decode as schema: %v", err)
		return v
	}

	return convertSchema(&nested)
}

func convertOAuthFlows(flows *shared.OAuthFlows) *OAuthFlows {
	if flows == nil {
		return nil
	}

	result := &OAuthFlows{}

	if flows.Implicit != nil {
		result.Implicit = &OAuthFlow{
			AuthorizationURL: flows.Implicit.AuthorizationURL,
			TokenURL:         flows.Implicit.TokenURL,
			RefreshURL:       flows.Implicit.RefreshURL,
			Scopes:           flows.Implicit.Scopes,
		}
	}

	if flows.Password != nil {
		result.Password = &OAuthFlow{
			AuthorizationURL: flows.Password.AuthorizationURL,
			TokenURL:         flows.Password.TokenURL,
			RefreshURL:       flows.Password.RefreshURL,
			Scopes:           flows.Password.Scopes,
		}
	}

	if flows.ClientCredentials != nil {
		result.ClientCredentials = &OAuthFlow{
			AuthorizationURL: flows.ClientCredentials.AuthorizationURL,
			TokenURL:         flows.ClientCredentials.TokenURL,
			RefreshURL:       flows.ClientCredentials.RefreshURL,
			Scopes:           flows.ClientCredentials.Scopes,
		}
	}

	if flows.AuthorizationCode != nil {
		result.AuthorizationCode = &OAuthFlow{
			AuthorizationURL: flows.AuthorizationCode.AuthorizationURL,
			TokenURL:         flows.AuthorizationCode.TokenURL,
			RefreshURL:       flows.AuthorizationCode.RefreshURL,
			Scopes:           flows.AuthorizationCode.Scopes,
		}
	}

	return result
}

func convertExamples(examples map[string]*shared.Example) map[string]*Example {
	if examples == nil {
		return nil
	}

	result := make(map[string]*Example)
	for k, v := range examples {
		result[k] = &Example{
			Summary:     v.Summary,
			Description: v.Description,
			Value:       v.Value,
		}
	}

	return result
}

func convertWebSocketChannel(spec *APISpec, opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) WebSocketEndpoint {
	ws := WebSocketEndpoint{
		ID:          opID,
		Path:        channel.Address,
		Summary:     channel.Summary,
		Description: channel.Description,
		Tags:        extractAsyncTagNames(channel.Tags),
		Metadata:    make(map[string]any),
	}

	// Sorted, because the assignments below are last-write-wins: a channel
	// declaring several messages otherwise left SendSchema holding whichever
	// payload the map surrendered last, which changed run to run.
	for _, msgName := range sortedStringKeys(channel.Messages) {
		if msg := channel.Messages[msgName]; msg.Payload != nil {
			schema := convertSchema(msg.Payload)

			switch operation.Action {
			case "send":
				ws.SendSchema = schema
			case "receive":
				ws.ReceiveSchema = schema
			}

			if ws.Metadata["messages"] == nil {
				ws.Metadata["messages"] = make(map[string]string)
			}

			ws.Metadata["messages"].(map[string]string)[msgName] = operation.Action
		}
	}

	ws.StreamBindings = streamBindings(channel.Extensions)
	registerStreamBindingEntities(spec, channel.Address, ws.StreamBindings)

	return ws
}

func convertSSEChannel(spec *APISpec, opID string, channel *shared.AsyncAPIChannel, operation *shared.AsyncAPIOperation) SSEEndpoint {
	sse := SSEEndpoint{
		ID:           opID,
		Path:         channel.Address,
		Summary:      channel.Summary,
		Description:  channel.Description,
		Tags:         extractAsyncTagNames(channel.Tags),
		EventSchemas: make(map[string]*Schema),
		Metadata:     make(map[string]any),
	}

	for msgName, msg := range channel.Messages {
		if msg.Payload != nil {
			sse.EventSchemas[msgName] = convertSchema(msg.Payload)
		}
	}

	sse.StreamBindings = streamBindings(channel.Extensions)
	registerStreamBindingEntities(spec, channel.Address, sse.StreamBindings)

	return sse
}

func detectWebSocketChannel(asyncAPI *shared.AsyncAPISpec, channel *shared.AsyncAPIChannel) bool {
	for _, serverRef := range channel.Servers {
		serverName := strings.TrimPrefix(serverRef.Ref, "#/servers/")
		if server, ok := asyncAPI.Servers[serverName]; ok {
			protocol := strings.ToLower(server.Protocol)
			if protocol == "ws" || protocol == "wss" {
				return true
			}
		}
	}

	return len(channel.Messages) > 0
}

func extractAsyncTagNames(tags []shared.AsyncAPITag) []string {
	names := make([]string, len(tags))
	for i, tag := range tags {
		names[i] = tag.Name
	}

	return names
}
