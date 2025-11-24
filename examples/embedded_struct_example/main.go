package main

import (
	"fmt"
	"log"

	"github.com/xraph/forge"
)

// PaginationParams is a reusable embedded struct for pagination
type PaginationParams struct {
	Page  int `query:"page" json:"page,omitempty" description:"Page number" default:"1"`
	Limit int `query:"limit" json:"limit,omitempty" description:"Items per page" default:"10"`
}

// BaseFilter is a reusable embedded struct for sorting
type BaseFilter struct {
	SortBy    string `query:"sort_by" json:"sort_by,omitempty" description:"Field to sort by"`
	SortOrder string `query:"sort_order" json:"sort_order,omitempty" description:"Sort order (asc/desc)" enum:"asc,desc"`
}

// ListWorkspacesRequest embeds both PaginationParams and BaseFilter
// All fields from embedded structs are automatically flattened
type ListWorkspacesRequest struct {
	PaginationParams
	BaseFilter
	Plan string `query:"plan" json:"plan,omitempty" description:"Filter by plan" enum:"free,pro,enterprise"`
}

// Workspace represents a workspace entity
type Workspace struct {
	ID        string `json:"id" description:"Workspace ID"`
	Name      string `json:"name" description:"Workspace name"`
	Plan      string `json:"plan" description:"Subscription plan"`
	CreatedAt string `json:"created_at" description:"Creation timestamp"`
}

// ListWorkspacesResponse is the response for listing workspaces
type ListWorkspacesResponse struct {
	Workspaces []Workspace `json:"workspaces" description:"List of workspaces"`
	Total      int         `json:"total" description:"Total number of workspaces"`
	Page       int         `json:"page" description:"Current page"`
	TotalPages int         `json:"total_pages" description:"Total number of pages"`
}

func main() {
	// Create Forge application with OpenAPI
	app := forge.New(
		forge.WithOpenAPI(forge.OpenAPIConfig{
			Title:       "Workspace API",
			Description: "Example API demonstrating embedded struct flattening",
			Version:     "1.0.0",
		}),
	)

	router := app.Router()

	// Register route with embedded struct request
	// All query parameters from embedded structs will be automatically extracted:
	// - page, limit from PaginationParams
	// - sort_by, sort_order from BaseFilter
	// - plan from ListWorkspacesRequest
	router.GET("/workspaces", listWorkspaces,
		forge.WithName("list-workspaces"),
		forge.WithSummary("List workspaces"),
		forge.WithDescription("Get a paginated list of workspaces with filtering and sorting"),
		forge.WithTags("workspaces"),
		forge.WithRequestSchema(ListWorkspacesRequest{}),
		forge.WithResponse(200, "List of workspaces", ListWorkspacesResponse{}),
	)

	// OpenAPI spec will be available at http://localhost:8080/openapi.json
	// Swagger UI will be available at http://localhost:8080/swagger
	fmt.Println("Server starting on http://localhost:8080")
	fmt.Println("OpenAPI Spec: http://localhost:8080/openapi.json")
	fmt.Println("Swagger UI: http://localhost:8080/swagger")
	fmt.Println("")
	fmt.Println("Example requests:")
	fmt.Println("  curl 'http://localhost:8080/workspaces?page=1&limit=10'")
	fmt.Println("  curl 'http://localhost:8080/workspaces?page=1&limit=10&plan=pro&sort_by=name&sort_order=asc'")

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func listWorkspaces(ctx forge.Context) error {
	// Bind request with embedded struct fields
	var req ListWorkspacesRequest
	if err := ctx.BindRequest(&req); err != nil {
		return err
	}

	// Log the parsed request
	ctx.Logger().Info("Listing workspaces",
		"page", req.Page,
		"limit", req.Limit,
		"plan", req.Plan,
		"sort_by", req.SortBy,
		"sort_order", req.SortOrder,
	)

	// Mock response
	workspaces := []Workspace{
		{
			ID:        "ws-1",
			Name:      "My Workspace",
			Plan:      "pro",
			CreatedAt: "2024-01-01T00:00:00Z",
		},
		{
			ID:        "ws-2",
			Name:      "Team Workspace",
			Plan:      "enterprise",
			CreatedAt: "2024-01-02T00:00:00Z",
		},
	}

	// Filter by plan if provided
	if req.Plan != "" {
		filtered := make([]Workspace, 0)
		for _, ws := range workspaces {
			if ws.Plan == req.Plan {
				filtered = append(filtered, ws)
			}
		}
		workspaces = filtered
	}

	response := ListWorkspacesResponse{
		Workspaces: workspaces,
		Total:      len(workspaces),
		Page:       req.Page,
		TotalPages: 1,
	}

	return ctx.JSON(200, response)
}

