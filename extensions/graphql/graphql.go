package graphql

import (
	"context"
	"net/http"
)

// GraphQL represents a unified GraphQL server interface (wraps gqlgen).
type GraphQL interface {
	// Schema management
	RegisterType(name string, obj any) error
	RegisterQuery(name string, resolver FieldResolverFunc) error
	RegisterMutation(name string, resolver FieldResolverFunc) error
	RegisterSubscription(name string, resolver SubscriptionResolverFunc) error

	// Schema generation
	GenerateSchema() (string, error)
	GetSchema() *GraphQLSchema

	// Execution
	ExecuteQuery(ctx context.Context, query string, variables map[string]any) (*Response, error)

	// HTTP handlers
	HTTPHandler() http.Handler
	PlaygroundHandler() http.Handler

	// Middleware
	Use(middleware Middleware)

	// Introspection
	EnableIntrospection(enable bool)

	// Health
	Ping(ctx context.Context) error
}

// FieldResolverFunc handles field resolution (for dynamic registration).
type FieldResolverFunc func(ctx context.Context, args map[string]any) (any, error)

// SubscriptionResolverFunc handles subscription events (for dynamic registration).
type SubscriptionResolverFunc func(ctx context.Context, args map[string]any) (<-chan any, error)

// Middleware wraps GraphQL execution.
type Middleware func(next ExecutorFunc) ExecutorFunc

// ExecutorFunc executes a GraphQL operation.
type ExecutorFunc func(ctx context.Context, req *Request) (*Response, error)

// GraphQLSchema represents a GraphQL schema (wrapper for introspection).
type GraphQLSchema struct {
	Types         map[string]*TypeDefinition
	Queries       map[string]*FieldDefinition
	Mutations     map[string]*FieldDefinition
	Subscriptions map[string]*FieldDefinition
	Directives    []*GraphQLDirective
}

// TypeDefinition defines a GraphQL type.
type TypeDefinition struct {
	Name        string
	Description string
	Kind        string // OBJECT, INTERFACE, UNION, ENUM, INPUT_OBJECT, SCALAR
	Fields      []*FieldDefinition
	Interfaces  []string
}

// FieldDefinition defines a field in a type.
type FieldDefinition struct {
	Name              string
	Description       string
	Type              string
	Args              []*ArgumentDefinition
	Resolver          FieldResolverFunc
	Deprecated        bool
	DeprecationReason string
}

// ArgumentDefinition defines an argument.
type ArgumentDefinition struct {
	Name         string
	Description  string
	Type         string
	DefaultValue any
}

// GraphQLDirective defines a GraphQL directive (wrapper for introspection).
type GraphQLDirective struct {
	Name        string
	Description string
	Locations   []string
	Args        []*ArgumentDefinition
}

// Request represents a GraphQL request.
type Request struct {
	Query         string         `json:"query"`
	OperationName string         `json:"operationName,omitempty"`
	Variables     map[string]any `json:"variables,omitempty"`
}

// Response represents a GraphQL response.
type Response struct {
	Data       any            `json:"data,omitempty"`
	Errors     []Error        `json:"errors,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Error represents a GraphQL error.
type Error struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path,omitempty"`
	Locations  []Location     `json:"locations,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// Location represents an error location.
type Location struct {
	Line   int `json:"line"`
	Column int `json:"column"`
}

// DataLoader optimizes N+1 queries.
type DataLoader interface {
	Load(ctx context.Context, key any) (any, error)
	LoadMany(ctx context.Context, keys []any) ([]any, error)
	Prime(key any, value any)
	Clear(key any)
	ClearAll()
}

// FieldResolver resolves a specific field.
type FieldResolver interface {
	Resolve(ctx context.Context, source any, args map[string]any) (any, error)
}
