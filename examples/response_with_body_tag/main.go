package main

import (
	"log"

	"github.com/xraph/forge"
)

// Domain model
type Workspace struct {
	ID        string `json:"id" example:"ws-123"`
	Name      string `json:"name" example:"My Workspace"`
	CreatedAt string `json:"created_at,omitempty" example:"2024-01-01T00:00:00Z"`
}

// Pagination metadata
type PageMeta struct {
	Page  int `json:"page" example:"1"`
	Limit int `json:"limit" example:"20"`
	Total int `json:"total" example:"100"`
}

// Generic paginated response
type PaginatedResponse[T any] struct {
	Data []T       `json:"data"`
	Meta *PageMeta `json:"meta,omitempty"`
}

// Type alias for cleaner code
type ListWorkspacesResult = PaginatedResponse[*Workspace]

// EXAMPLE 1: Response with body:"" tag (unwraps the body)
// This will generate OpenAPI schema directly to PaginatedResponse, NOT a wrapper
type ListWorkspacesResponse struct {
	Body ListWorkspacesResult `json:"body" body:""`
}

// EXAMPLE 2: Response with headers AND body
type DetailedListResponse struct {
	CacheControl string               `header:"Cache-Control" example:"max-age=3600"`
	ETag         string               `header:"ETag" example:"\"abc123\""`
	RateLimit    int                  `header:"X-Rate-Limit" example:"100"`
	Body         ListWorkspacesResult `body:""`
}

// EXAMPLE 3: Response with optional headers
type CachedResponse struct {
	CacheControl string               `header:"Cache-Control" optional:"true" example:"max-age=3600"`
	LastModified string               `header:"Last-Modified" optional:"true" example:"Mon, 01 Jan 2024 00:00:00 GMT"`
	Body         ListWorkspacesResult `body:""`
}

// EXAMPLE 4: Headers-only response (redirect, etc)
type RedirectResponse struct {
	Location string `header:"Location" example:"https://example.com"`
	XTraceID string `header:"X-Trace-ID" optional:"true" example:"trace-123"`
}

// EXAMPLE 5: Traditional response (no unwrapping)
type TraditionalResponse struct {
	Data []Workspace `json:"data"`
	Meta *PageMeta   `json:"meta,omitempty"`
}

func main() {
	app := forge.New()
	router := app.Router()

	// Example 1: Simple unwrapped response
	router.GET("/api/workspaces", handleListWorkspaces,
		forge.WithDescription("List workspaces with unwrapped response body"),
		forge.WithResponseSchema(200, "List of workspaces", ListWorkspacesResponse{}),
		forge.WithTags("Workspaces"),
	)

	// Example 2: Response with headers and body
	router.GET("/api/workspaces/detailed", handleDetailedList,
		forge.WithDescription("List workspaces with cache headers"),
		forge.WithResponseSchema(200, "List of workspaces with cache headers", DetailedListResponse{}),
		forge.WithTags("Workspaces"),
	)

	// Example 3: Response with optional headers
	router.GET("/api/workspaces/cached", handleCachedList,
		forge.WithDescription("List workspaces with optional cache headers"),
		forge.WithResponseSchema(200, "Cached list of workspaces", CachedResponse{}),
		forge.WithTags("Workspaces"),
	)

	// Example 4: Headers-only response
	router.POST("/api/workspaces/:id/redirect", handleRedirect,
		forge.WithDescription("Redirect to workspace"),
		forge.WithResponseSchema(302, "Redirect to workspace", RedirectResponse{}),
		forge.WithTags("Workspaces"),
	)

	// Example 5: Traditional response (no unwrapping)
	router.GET("/api/workspaces/traditional", handleTraditional,
		forge.WithDescription("List workspaces (traditional response)"),
		forge.WithResponseSchema(200, "Traditional response", TraditionalResponse{}),
		forge.WithTags("Workspaces"),
	)

	log.Printf("Server starting on :8080")
	log.Printf("OpenAPI docs: http://localhost:8080/openapi")
	log.Fatal(app.Run())
}

func handleListWorkspaces(ctx forge.Context) error {
	workspaces := []*Workspace{
		{ID: "ws-1", Name: "Workspace 1", CreatedAt: "2024-01-01T00:00:00Z"},
		{ID: "ws-2", Name: "Workspace 2", CreatedAt: "2024-01-02T00:00:00Z"},
	}

	return ctx.JSON(200, PaginatedResponse[*Workspace]{
		Data: workspaces,
		Meta: &PageMeta{Page: 1, Limit: 20, Total: 2},
	})
}

func handleDetailedList(ctx forge.Context) error {
	ctx.SetHeader("Cache-Control", "max-age=3600")
	ctx.SetHeader("ETag", "\"abc123\"")
	ctx.SetHeader("X-Rate-Limit", "100")

	workspaces := []*Workspace{
		{ID: "ws-1", Name: "Workspace 1"},
	}

	return ctx.JSON(200, PaginatedResponse[*Workspace]{
		Data: workspaces,
		Meta: &PageMeta{Page: 1, Limit: 20, Total: 1},
	})
}

func handleCachedList(ctx forge.Context) error {
	// Optional headers may or may not be set
	ctx.SetHeader("Cache-Control", "max-age=3600")

	workspaces := []*Workspace{
		{ID: "ws-1", Name: "Workspace 1"},
	}

	return ctx.JSON(200, PaginatedResponse[*Workspace]{
		Data: workspaces,
		Meta: &PageMeta{Page: 1, Limit: 20, Total: 1},
	})
}

func handleRedirect(ctx forge.Context) error {
	ctx.SetHeader("Location", "https://example.com/workspaces/ws-1")
	ctx.SetHeader("X-Trace-ID", "trace-123")
	return ctx.NoContent(302)
}

func handleTraditional(ctx forge.Context) error {
	workspaces := []Workspace{
		{ID: "ws-1", Name: "Workspace 1"},
	}

	return ctx.JSON(200, TraditionalResponse{
		Data: workspaces,
		Meta: &PageMeta{Page: 1, Limit: 20, Total: 1},
	})
}

