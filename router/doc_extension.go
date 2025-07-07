package router

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Extended documentation generator methods

// GetOpenAPIYAML returns the OpenAPI spec as YAML
func (g *DocumentationGenerator) GetOpenAPIYAML() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.openAPISpec == nil {
		return nil, fmt.Errorf("OpenAPI spec not generated")
	}

	return yaml.Marshal(g.openAPISpec)
}

// GetAsyncAPIYAML returns the AsyncAPI spec as YAML
func (g *DocumentationGenerator) GetAsyncAPIYAML() ([]byte, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.asyncAPISpec == nil {
		return nil, fmt.Errorf("AsyncAPI spec not generated")
	}

	return yaml.Marshal(g.asyncAPISpec)
}

// GetPostmanCollection generates a Postman collection from the OpenAPI spec
func (g *DocumentationGenerator) GetPostmanCollection() (*PostmanCollection, error) {
	g.mu.RLock()
	defer g.mu.RUnlock()

	if g.openAPISpec == nil {
		return nil, fmt.Errorf("OpenAPI spec not generated")
	}

	collection := &PostmanCollection{
		Info: PostmanInfo{
			Name:        g.openAPISpec.Info.Title,
			Description: g.openAPISpec.Info.Description,
			Version:     g.openAPISpec.Info.Version,
			Schema:      "https://schema.getpostman.com/json/collection/v2.1.0/collection.json",
		},
		Item:     make([]PostmanItem, 0),
		Variable: make([]PostmanVariable, 0),
		Auth:     g.generatePostmanAuth(),
	}

	// Add server variables
	if len(g.openAPISpec.Servers) > 0 {
		server := g.openAPISpec.Servers[0]
		collection.Variable = append(collection.Variable, PostmanVariable{
			Key:   "baseUrl",
			Value: server.URL,
			Type:  "string",
		})
	}

	// Convert paths to Postman items
	for path, pathItem := range g.openAPISpec.Paths {
		g.addPostmanPathItem(collection, path, pathItem)
	}

	return collection, nil
}

// generatePostmanAuth generates authentication configuration for Postman
func (g *DocumentationGenerator) generatePostmanAuth() *PostmanAuth {
	// Check if there are security definitions
	if len(g.config.SecurityDefinitions) == 0 {
		return nil
	}

	// Use the first security definition for collection-level auth
	for _, scheme := range g.config.SecurityDefinitions {
		switch scheme.Type {
		case "http":
			if scheme.Scheme == "bearer" {
				return &PostmanAuth{
					Type: "bearer",
					Bearer: []PostmanAuthAttribute{
						{Key: "token", Value: "{{bearerToken}}", Type: "string"},
					},
				}
			}
		case "apiKey":
			return &PostmanAuth{
				Type: "apikey",
				APIKey: []PostmanAuthAttribute{
					{Key: "key", Value: scheme.Name, Type: "string"},
					{Key: "value", Value: "{{apiKey}}", Type: "string"},
					{Key: "in", Value: scheme.In, Type: "string"},
				},
			}
		case "oauth2":
			return &PostmanAuth{
				Type: "oauth2",
				OAuth2: []PostmanAuthAttribute{
					{Key: "accessToken", Value: "{{accessToken}}", Type: "string"},
					{Key: "tokenType", Value: "Bearer", Type: "string"},
				},
			}
		}
		// Use first applicable scheme
		break
	}

	return nil
}

// addPostmanPathItem adds a path item to the Postman collection
func (g *DocumentationGenerator) addPostmanPathItem(collection *PostmanCollection, path string, pathItem PathItem) {
	// Create a folder for the path if it has multiple operations
	operations := g.getPathOperations(pathItem)
	if len(operations) > 1 {
		folder := PostmanItem{
			Name:        path,
			Description: fmt.Sprintf("Operations for %s", path),
			Item:        make([]PostmanItem, 0),
		}

		for method, operation := range operations {
			request := g.createPostmanRequest(method, path, operation)
			folder.Item = append(folder.Item, PostmanItem{
				Name:        fmt.Sprintf("%s %s", strings.ToUpper(method), path),
				Description: operation.Summary,
				Request:     request,
			})
		}

		collection.Item = append(collection.Item, folder)
	} else {
		// Single operation - add directly
		for method, operation := range operations {
			request := g.createPostmanRequest(method, path, operation)
			collection.Item = append(collection.Item, PostmanItem{
				Name:        fmt.Sprintf("%s %s", strings.ToUpper(method), path),
				Description: operation.Summary,
				Request:     request,
			})
		}
	}
}

// getPathOperations extracts operations from a path item
func (g *DocumentationGenerator) getPathOperations(pathItem PathItem) map[string]*Operation {
	operations := make(map[string]*Operation)

	if pathItem.Get != nil {
		operations["get"] = pathItem.Get
	}
	if pathItem.Post != nil {
		operations["post"] = pathItem.Post
	}
	if pathItem.Put != nil {
		operations["put"] = pathItem.Put
	}
	if pathItem.Patch != nil {
		operations["patch"] = pathItem.Patch
	}
	if pathItem.Delete != nil {
		operations["delete"] = pathItem.Delete
	}
	if pathItem.Head != nil {
		operations["head"] = pathItem.Head
	}
	if pathItem.Options != nil {
		operations["options"] = pathItem.Options
	}

	return operations
}

// createPostmanRequest creates a Postman request from an operation
func (g *DocumentationGenerator) createPostmanRequest(method, path string, operation *Operation) *PostmanRequest {
	request := &PostmanRequest{
		Method: strings.ToUpper(method),
		Header: make([]PostmanHeader, 0),
		URL: &PostmanURL{
			Raw:      "{{baseUrl}}" + path,
			Protocol: "https",
			Host:     []string{"{{baseUrl}}"},
			Path:     strings.Split(strings.Trim(path, "/"), "/"),
			Query:    make([]PostmanQuery, 0),
		},
		Description: operation.Description,
	}

	// Add parameters
	for _, param := range operation.Parameters {
		switch param.In {
		case "query":
			request.URL.Query = append(request.URL.Query, PostmanQuery{
				Key:         param.Name,
				Value:       g.getExampleValue(param.Schema),
				Description: param.Description,
				Disabled:    !param.Required,
			})
		case "header":
			request.Header = append(request.Header, PostmanHeader{
				Key:         param.Name,
				Value:       g.getExampleValue(param.Schema),
				Description: param.Description,
				Disabled:    !param.Required,
			})
		case "path":
			// Path parameters are handled in URL
			continue
		}
	}

	// Add request body
	if operation.RequestBody != nil {
		request.Body = g.createPostmanBody(operation.RequestBody)

		// Add content-type header
		for contentType := range operation.RequestBody.Content {
			request.Header = append(request.Header, PostmanHeader{
				Key:   "Content-Type",
				Value: contentType,
			})
			break // Use first content type
		}
	}

	// Add common headers
	request.Header = append(request.Header, PostmanHeader{
		Key:   "Accept",
		Value: "application/json",
	})

	return request
}

// createPostmanBody creates a Postman request body
func (g *DocumentationGenerator) createPostmanBody(requestBody *RequestBody) *PostmanBody {
	body := &PostmanBody{
		Mode: "raw",
		Raw:  "",
	}

	// Use JSON content type if available
	if mediaType, ok := requestBody.Content["application/json"]; ok {
		body.Mode = "raw"
		body.Raw = g.generateJSONExample(mediaType.Schema)
		body.Options = &PostmanBodyOptions{
			Raw: PostmanBodyRaw{
				Language: "json",
			},
		}
	} else if mediaType, ok := requestBody.Content["application/x-www-form-urlencoded"]; ok {
		body.Mode = "urlencoded"
		body.URLEncoded = g.generateFormData(mediaType.Schema)
	} else if mediaType, ok := requestBody.Content["multipart/form-data"]; ok {
		body.Mode = "formdata"
		body.FormData = g.generateFormData(mediaType.Schema)
	}

	return body
}

// generateJSONExample generates a JSON example from a schema
func (g *DocumentationGenerator) generateJSONExample(schema *Schema) string {
	example := g.generateSchemaExample(schema)
	jsonData, err := json.MarshalIndent(example, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(jsonData)
}

// generateSchemaExample generates an example value from a schema
func (g *DocumentationGenerator) generateSchemaExample(schema *Schema) interface{} {
	if schema == nil {
		return nil
	}

	switch schema.Type {
	case "string":
		if schema.Format == "date-time" {
			return time.Now().Format(time.RFC3339)
		} else if schema.Format == "date" {
			return time.Now().Format("2006-01-02")
		} else if schema.Format == "email" {
			return "user@example.com"
		} else if schema.Format == "uri" {
			return "https://example.com"
		}
		return "string"
	case "integer":
		return 42
	case "number":
		return 3.14
	case "boolean":
		return true
	case "array":
		if schema.Items != nil {
			return []interface{}{g.generateSchemaExample(schema.Items)}
		}
		return []interface{}{}
	case "object":
		obj := make(map[string]interface{})
		for propName, propSchema := range schema.Properties {
			obj[propName] = g.generateSchemaExample(propSchema)
		}
		return obj
	default:
		return nil
	}
}

// generateFormData generates form data from a schema
func (g *DocumentationGenerator) generateFormData(schema *Schema) []PostmanFormData {
	formData := make([]PostmanFormData, 0)

	if schema != nil && schema.Type == "object" {
		for propName, propSchema := range schema.Properties {
			formData = append(formData, PostmanFormData{
				Key:   propName,
				Value: fmt.Sprintf("%v", g.generateSchemaExample(propSchema)),
				Type:  "text",
			})
		}
	}

	return formData
}

// getExampleValue gets an example value for a parameter
func (g *DocumentationGenerator) getExampleValue(schema *Schema) string {
	if schema == nil {
		return ""
	}

	example := g.generateSchemaExample(schema)
	return fmt.Sprintf("%v", example)
}

// Schema reflection utilities
func (g *DocumentationGenerator) ReflectSchema(t reflect.Type) *Schema {
	g.mu.Lock()
	defer g.mu.Unlock()

	// Check cache first
	if schema, ok := g.schemaCache[t]; ok {
		return schema
	}

	schema := g.reflectSchemaRecursive(t, make(map[reflect.Type]bool))
	g.schemaCache[t] = schema
	return schema
}

func (g *DocumentationGenerator) reflectSchemaRecursive(t reflect.Type, visited map[reflect.Type]bool) *Schema {
	// Handle pointers
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Check for circular references
	if visited[t] {
		return &Schema{
			Type:        "object",
			Description: fmt.Sprintf("Circular reference to %s", t.Name()),
		}
	}
	visited[t] = true

	schema := &Schema{}

	switch t.Kind() {
	case reflect.String:
		schema.Type = "string"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		schema.Type = "integer"
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		schema.Type = "integer"
	case reflect.Float32, reflect.Float64:
		schema.Type = "number"
	case reflect.Bool:
		schema.Type = "boolean"
	case reflect.Slice, reflect.Array:
		schema.Type = "array"
		schema.Items = g.reflectSchemaRecursive(t.Elem(), visited)
	case reflect.Map:
		schema.Type = "object"
		schema.AdditionalProperties = g.reflectSchemaRecursive(t.Elem(), visited)
	case reflect.Struct:
		schema.Type = "object"
		schema.Properties = make(map[string]*Schema)
		schema.Required = make([]string, 0)

		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)

			// Skip unexported fields
			if !field.IsExported() {
				continue
			}

			// Get field name from JSON tag or use field name
			jsonTag := field.Tag.Get("json")
			fieldName := field.Name

			if jsonTag != "" {
				parts := strings.Split(jsonTag, ",")
				if parts[0] != "" && parts[0] != "-" {
					fieldName = parts[0]
				}

				// Skip fields with json:"-"
				if jsonTag == "-" {
					continue
				}
			}

			// Create field schema
			fieldSchema := g.reflectSchemaRecursive(field.Type, visited)

			// Add validation from struct tags
			if validateTag := field.Tag.Get("validate"); validateTag != "" {
				g.parseValidationTag(fieldSchema, validateTag)
			}

			// Add description from comment or tag
			if desc := field.Tag.Get("description"); desc != "" {
				fieldSchema.Description = desc
			}

			schema.Properties[fieldName] = fieldSchema

			// Check if field is required
			if g.isFieldRequired(field) {
				schema.Required = append(schema.Required, fieldName)
			}
		}
	case reflect.Interface:
		schema.Type = "object"
		schema.Description = "Interface type"
	default:
		schema.Type = "object"
		schema.Description = fmt.Sprintf("Unknown type: %s", t.Kind())
	}

	delete(visited, t)
	return schema
}

// parseValidationTag parses validation tags and adds constraints to schema
func (g *DocumentationGenerator) parseValidationTag(schema *Schema, tag string) {
	rules := strings.Split(tag, ",")
	for _, rule := range rules {
		rule = strings.TrimSpace(rule)

		if rule == "required" {
			// Required is handled at the parent level
			continue
		}

		if strings.HasPrefix(rule, "min=") {
			// Handle minimum constraints
			continue
		}

		if strings.HasPrefix(rule, "max=") {
			// Handle maximum constraints
			continue
		}

		if strings.HasPrefix(rule, "len=") {
			// Handle length constraints
			continue
		}

		if rule == "email" {
			schema.Format = "email"
		}

		if rule == "url" {
			schema.Format = "uri"
		}

		if rule == "uuid" {
			schema.Format = "uuid"
		}
	}
}

// isFieldRequired checks if a struct field is required
func (g *DocumentationGenerator) isFieldRequired(field reflect.StructField) bool {
	jsonTag := field.Tag.Get("json")
	if jsonTag != "" {
		parts := strings.Split(jsonTag, ",")
		for _, part := range parts {
			if part == "omitempty" {
				return false
			}
		}
	}

	validateTag := field.Tag.Get("validate")
	if validateTag != "" {
		rules := strings.Split(validateTag, ",")
		for _, rule := range rules {
			if strings.TrimSpace(rule) == "required" {
				return true
			}
		}
	}

	// Check if field is a pointer (optional by default)
	if field.Type.Kind() == reflect.Ptr {
		return false
	}

	return true
}

// RegisterSchema registers a schema with a name for reuse
func (g *DocumentationGenerator) RegisterSchema(name string, schema *Schema) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.schemas[name] = schema
}

// RegisterSchemaFromType registers a schema from a Go type
func (g *DocumentationGenerator) RegisterSchemaFromType(name string, t reflect.Type) {
	schema := g.ReflectSchema(t)
	g.RegisterSchema(name, schema)
}

// GetRegisteredSchemas returns all registered schemas
func (g *DocumentationGenerator) GetRegisteredSchemas() map[string]*Schema {
	g.mu.RLock()
	defer g.mu.RUnlock()

	schemas := make(map[string]*Schema)
	for name, schema := range g.schemas {
		schemas[name] = schema
	}
	return schemas
}

// Components represents OpenAPI components
type Components struct {
	Schemas         map[string]*Schema         `json:"schemas,omitempty"`
	Responses       map[string]*Response       `json:"responses,omitempty"`
	Parameters      map[string]*Parameter      `json:"parameters,omitempty"`
	Examples        map[string]*Example        `json:"examples,omitempty"`
	RequestBodies   map[string]*RequestBody    `json:"requestBodies,omitempty"`
	Headers         map[string]*Header         `json:"headers,omitempty"`
	SecuritySchemes map[string]*SecurityScheme `json:"securitySchemes,omitempty"`
	Links           map[string]*Link           `json:"links,omitempty"`
	Callbacks       map[string]*Callback       `json:"callbacks,omitempty"`
}

// Link represents OpenAPI link
type Link struct {
	OperationRef string                 `json:"operationRef,omitempty"`
	OperationID  string                 `json:"operationId,omitempty"`
	Parameters   map[string]interface{} `json:"parameters,omitempty"`
	RequestBody  interface{}            `json:"requestBody,omitempty"`
	Description  string                 `json:"description,omitempty"`
	Server       *ServerInfo            `json:"server,omitempty"`
}

// Callback represents OpenAPI callback
type Callback map[string]*PathItem

// Schema fields for OpenAPI 3.1
type Schema struct {
	// Basic fields
	Type        string      `json:"type,omitempty"`
	Format      string      `json:"format,omitempty"`
	Title       string      `json:"title,omitempty"`
	Description string      `json:"description,omitempty"`
	Default     interface{} `json:"default,omitempty"`
	Example     interface{} `json:"example,omitempty"`

	// Reference
	Ref string `json:"$ref,omitempty"`

	// Validation
	MultipleOf       float64       `json:"multipleOf,omitempty"`
	Maximum          float64       `json:"maximum,omitempty"`
	ExclusiveMaximum bool          `json:"exclusiveMaximum,omitempty"`
	Minimum          float64       `json:"minimum,omitempty"`
	ExclusiveMinimum bool          `json:"exclusiveMinimum,omitempty"`
	MaxLength        int           `json:"maxLength,omitempty"`
	MinLength        int           `json:"minLength,omitempty"`
	Pattern          string        `json:"pattern,omitempty"`
	MaxItems         int           `json:"maxItems,omitempty"`
	MinItems         int           `json:"minItems,omitempty"`
	UniqueItems      bool          `json:"uniqueItems,omitempty"`
	MaxProperties    int           `json:"maxProperties,omitempty"`
	MinProperties    int           `json:"minProperties,omitempty"`
	Required         []string      `json:"required,omitempty"`
	Enum             []interface{} `json:"enum,omitempty"`

	// Object properties
	Properties           map[string]*Schema `json:"properties,omitempty"`
	AdditionalProperties *Schema            `json:"additionalProperties,omitempty"`

	// Array items
	Items *Schema `json:"items,omitempty"`

	// Composition
	AllOf []*Schema `json:"allOf,omitempty"`
	OneOf []*Schema `json:"oneOf,omitempty"`
	AnyOf []*Schema `json:"anyOf,omitempty"`
	Not   *Schema   `json:"not,omitempty"`

	// Metadata
	ReadOnly     bool          `json:"readOnly,omitempty"`
	WriteOnly    bool          `json:"writeOnly,omitempty"`
	Deprecated   bool          `json:"deprecated,omitempty"`
	ExternalDocs *ExternalDocs `json:"externalDocs,omitempty"`

	// Extensions
	Extensions map[string]interface{} `json:"-"`
}

// Response represents OpenAPI response with complete fields
type Response struct {
	Description string                `json:"description"`
	Headers     map[string]*Header    `json:"headers,omitempty"`
	Content     map[string]*MediaType `json:"content,omitempty"`
	Links       map[string]*Link      `json:"links,omitempty"`
	Schema      *Schema
	Extensions  map[string]interface{} `json:"-"`
}

// PostmanCollection Postman Collection types
type PostmanCollection struct {
	Info     PostmanInfo       `json:"info"`
	Item     []PostmanItem     `json:"item"`
	Event    []PostmanEvent    `json:"event,omitempty"`
	Variable []PostmanVariable `json:"variable,omitempty"`
	Auth     *PostmanAuth      `json:"auth,omitempty"`
}

type PostmanInfo struct {
	Name        string `json:"name"`
	PostmanID   string `json:"_postman_id,omitempty"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version,omitempty"`
	Schema      string `json:"schema"`
}

type PostmanItem struct {
	Name        string            `json:"name"`
	Description string            `json:"description,omitempty"`
	Item        []PostmanItem     `json:"item,omitempty"`
	Request     *PostmanRequest   `json:"request,omitempty"`
	Response    []PostmanResponse `json:"response,omitempty"`
	Event       []PostmanEvent    `json:"event,omitempty"`
	Variable    []PostmanVariable `json:"variable,omitempty"`
	Auth        *PostmanAuth      `json:"auth,omitempty"`
}

type PostmanRequest struct {
	URL         *PostmanURL     `json:"url"`
	Method      string          `json:"method"`
	Header      []PostmanHeader `json:"header,omitempty"`
	Body        *PostmanBody    `json:"body,omitempty"`
	Auth        *PostmanAuth    `json:"auth,omitempty"`
	Description string          `json:"description,omitempty"`
}

type PostmanURL struct {
	Raw      string            `json:"raw"`
	Protocol string            `json:"protocol,omitempty"`
	Host     []string          `json:"host,omitempty"`
	Port     string            `json:"port,omitempty"`
	Path     []string          `json:"path,omitempty"`
	Query    []PostmanQuery    `json:"query,omitempty"`
	Hash     string            `json:"hash,omitempty"`
	Variable []PostmanVariable `json:"variable,omitempty"`
}

type PostmanQuery struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Description string `json:"description,omitempty"`
}

type PostmanHeader struct {
	Key         string `json:"key"`
	Value       string `json:"value"`
	Disabled    bool   `json:"disabled,omitempty"`
	Description string `json:"description,omitempty"`
}

type PostmanBody struct {
	Mode       string              `json:"mode"`
	Raw        string              `json:"raw,omitempty"`
	URLEncoded []PostmanFormData   `json:"urlencoded,omitempty"`
	FormData   []PostmanFormData   `json:"formdata,omitempty"`
	File       *PostmanFile        `json:"file,omitempty"`
	GraphQL    *PostmanGraphQL     `json:"graphql,omitempty"`
	Options    *PostmanBodyOptions `json:"options,omitempty"`
	Disabled   bool                `json:"disabled,omitempty"`
}

type PostmanFormData struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Type        string `json:"type"`
	Src         string `json:"src,omitempty"`
	Disabled    bool   `json:"disabled,omitempty"`
	Description string `json:"description,omitempty"`
}

type PostmanFile struct {
	Src     string `json:"src"`
	Content string `json:"content,omitempty"`
}

type PostmanGraphQL struct {
	Query     string                 `json:"query"`
	Variables map[string]interface{} `json:"variables,omitempty"`
}

type PostmanBodyOptions struct {
	Raw PostmanBodyRaw `json:"raw,omitempty"`
}

type PostmanBodyRaw struct {
	Language string `json:"language,omitempty"`
}

type PostmanResponse struct {
	Name                   string          `json:"name"`
	OriginalRequest        *PostmanRequest `json:"originalRequest"`
	Status                 string          `json:"status"`
	Code                   int             `json:"code"`
	PostmanPreviewlanguage string          `json:"_postman_previewlanguage,omitempty"`
	Header                 []PostmanHeader `json:"header,omitempty"`
	Cookie                 []PostmanCookie `json:"cookie,omitempty"`
	Body                   string          `json:"body,omitempty"`
}

type PostmanCookie struct {
	Domain     string        `json:"domain"`
	Expires    string        `json:"expires,omitempty"`
	HTTPOnly   bool          `json:"httpOnly,omitempty"`
	MaxAge     string        `json:"maxAge,omitempty"`
	Name       string        `json:"name"`
	Path       string        `json:"path"`
	Secure     bool          `json:"secure,omitempty"`
	Value      string        `json:"value"`
	Extensions []interface{} `json:"extensions,omitempty"`
}

type PostmanEvent struct {
	Listen   string         `json:"listen"`
	Script   *PostmanScript `json:"script"`
	ID       string         `json:"id,omitempty"`
	Disabled bool           `json:"disabled,omitempty"`
}

type PostmanScript struct {
	ID   string   `json:"id,omitempty"`
	Type string   `json:"type,omitempty"`
	Exec []string `json:"exec,omitempty"`
	Src  string   `json:"src,omitempty"`
}

type PostmanVariable struct {
	ID          string      `json:"id,omitempty"`
	Key         string      `json:"key"`
	Value       interface{} `json:"value,omitempty"`
	Type        string      `json:"type,omitempty"`
	Name        string      `json:"name,omitempty"`
	Description string      `json:"description,omitempty"`
	System      bool        `json:"system,omitempty"`
	Disabled    bool        `json:"disabled,omitempty"`
}

type PostmanAuth struct {
	Type   string                 `json:"type"`
	NoAuth interface{}            `json:"noauth,omitempty"`
	APIKey []PostmanAuthAttribute `json:"apikey,omitempty"`
	AWSV4  []PostmanAuthAttribute `json:"awsv4,omitempty"`
	Basic  []PostmanAuthAttribute `json:"basic,omitempty"`
	Bearer []PostmanAuthAttribute `json:"bearer,omitempty"`
	Digest []PostmanAuthAttribute `json:"digest,omitempty"`
	Hawk   []PostmanAuthAttribute `json:"hawk,omitempty"`
	NTLM   []PostmanAuthAttribute `json:"ntlm,omitempty"`
	OAuth1 []PostmanAuthAttribute `json:"oauth1,omitempty"`
	OAuth2 []PostmanAuthAttribute `json:"oauth2,omitempty"`
}

type PostmanAuthAttribute struct {
	Key   string      `json:"key"`
	Value interface{} `json:"value"`
	Type  string      `json:"type"`
}
