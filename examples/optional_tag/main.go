package main

import (
	"encoding/json"
	"fmt"
	"log"

	"github.com/xraph/forge"
)

// SortOrder represents the sort order direction
type SortOrder string

const (
	SortOrderAsc  SortOrder = "asc"
	SortOrderDesc SortOrder = "desc"
)

// BaseRequestParams contains common request parameters for sorting, searching, and filtering
// Can be used in both paginated and non-paginated requests
type BaseRequestParams struct {
	SortBy string    `json:"sortBy" query:"sort_by" default:"created_at" example:"created_at" optional:"true"`
	Order  SortOrder `json:"order" query:"order" default:"desc" validate:"oneof=asc desc" example:"desc" optional:"true"`
	Search string    `json:"search" query:"search" default:"" example:"john" optional:"true"`
	Filter string    `json:"filter" query:"filter" default:"" example:"status:active" optional:"true"`
	Fields string    `json:"fields" query:"fields" default:"" example:"id,name,email" optional:"true"`
}

// PaginationParams represents offset-based pagination request parameters
type PaginationParams struct {
	BaseRequestParams
	Limit  int `json:"limit" query:"limit" default:"10" validate:"min=1,max=100" example:"10" optional:"true"`
	Offset int `json:"offset" query:"offset" default:"0" validate:"min=0" example:"0" optional:"true"`
	Page   int `json:"page" query:"page" default:"1" validate:"min=1" example:"1" optional:"true"`
}

// CursorParams represents cursor-based pagination parameters
type CursorParams struct {
	BaseRequestParams
	Limit  int    `json:"limit" query:"limit" default:"10" validate:"min=1,max=100" example:"10" optional:"true"`
	Cursor string `json:"cursor" query:"cursor" default:"" example:"eyJpZCI6IjEyMyIsInRzIjoxNjQwMDAwMDAwfQ==" optional:"true"`
}

// PageMeta contains metadata for offset-based pagination
type PageMeta struct {
	TotalCount  int  `json:"totalCount" example:"100"`
	TotalPages  int  `json:"totalPages" example:"10"`
	CurrentPage int  `json:"currentPage" example:"1"`
	PerPage     int  `json:"perPage" example:"10"`
	HasNext     bool `json:"hasNext" example:"true"`
	HasPrev     bool `json:"hasPrev" example:"false"`
}

// CursorMeta contains metadata for cursor-based pagination
type CursorMeta struct {
	NextCursor string `json:"nextCursor,omitempty" example:"eyJpZCI6IjEyMyIsInRzIjoxNjQwMDAwMDAwfQ=="`
	PrevCursor string `json:"prevCursor,omitempty" example:"eyJpZCI6IjEwMCIsInRzIjoxNjM5OTk5OTk5fQ=="`
	HasNext    bool   `json:"hasNext" example:"true"`
	HasPrev    bool   `json:"hasPrev" example:"false"`
}

// PageResponse represents a paginated response with metadata
type PageResponse[T any] struct {
	Data       []T         `json:"data"`
	Pagination *PageMeta   `json:"pagination,omitempty"`
	Cursor     *CursorMeta `json:"cursor,omitempty"`
}

// User represents a user entity
type User struct {
	ID        string `json:"id" example:"123"`
	Name      string `json:"name" example:"John Doe"`
	Email     string `json:"email" example:"john@example.com"`
	Status    string `json:"status" example:"active"`
	CreatedAt string `json:"createdAt" example:"2024-01-01T00:00:00Z"`
}

func main() {
	// Create app with OpenAPI configuration
	app := forge.NewApp(forge.AppConfig{
		Name:        "optional-tag-demo",
		Version:     "1.0.0",
		Description: "Demonstration of optional tag feature for pagination",
		Environment: "development",
		HTTPAddress: ":8080",
		RouterOptions: []forge.RouterOption{
			forge.WithOpenAPI(forge.OpenAPIConfig{
				Title:       "Optional Tag Demo API",
				Description: "API demonstrating the optional tag for query parameters",
				Version:     "1.0.0",
				Servers: []forge.OpenAPIServer{
					{
						URL:         "http://localhost:8080",
						Description: "Development server",
					},
				},
			}),
		},
	})

	router := app.Router()

	// Route using offset-based pagination with optional fields
	router.GET("/users", func(ctx forge.Context) error {
		params := PaginationParams{}
		if err := ctx.Bind(&params); err != nil {
			return err
		}

		// All pagination fields are optional, so they can be omitted in the request
		// The defaults will be used if not provided
		fmt.Printf("Pagination params: %+v\n", params)

		// Create mock users
		users := []User{
			{ID: "1", Name: "John Doe", Email: "john@example.com", Status: "active", CreatedAt: "2024-01-01T00:00:00Z"},
			{ID: "2", Name: "Jane Smith", Email: "jane@example.com", Status: "active", CreatedAt: "2024-01-02T00:00:00Z"},
		}

		response := PageResponse[User]{
			Data: users,
			Pagination: &PageMeta{
				TotalCount:  100,
				TotalPages:  10,
				CurrentPage: params.Page,
				PerPage:     params.Limit,
				HasNext:     true,
				HasPrev:     params.Page > 1,
			},
		}

		return ctx.JSON(200, response)
	},
		forge.WithSummary("List users with offset-based pagination"),
		forge.WithDescription("Retrieve a paginated list of users with optional filtering, sorting, and searching"),
		forge.WithTags("users", "pagination"),
	)

	// Route using cursor-based pagination with optional fields
	router.GET("/users/cursor", func(ctx forge.Context) error {
		params := CursorParams{}
		if err := ctx.Bind(&params); err != nil {
			return err
		}

		// All fields are optional
		fmt.Printf("Cursor params: %+v\n", params)

		users := []User{
			{ID: "1", Name: "John Doe", Email: "john@example.com", Status: "active", CreatedAt: "2024-01-01T00:00:00Z"},
			{ID: "2", Name: "Jane Smith", Email: "jane@example.com", Status: "active", CreatedAt: "2024-01-02T00:00:00Z"},
		}

		response := PageResponse[User]{
			Data: users,
			Cursor: &CursorMeta{
				NextCursor: "eyJpZCI6IjIiLCJ0cyI6MTY0MDAwMDAwMH0=",
				PrevCursor: "",
				HasNext:    true,
				HasPrev:    false,
			},
		}

		return ctx.JSON(200, response)
	},
		forge.WithSummary("List users with cursor-based pagination"),
		forge.WithDescription("Retrieve a paginated list of users using cursor-based pagination with optional filtering"),
		forge.WithTags("users", "pagination", "cursor"),
	)

	// Generate and display OpenAPI spec
	spec := router.OpenAPISpec()
	if spec != nil {
		// Pretty print the spec
		specJSON, err := json.MarshalIndent(spec, "", "  ")
		if err != nil {
			log.Printf("Failed to marshal spec: %v", err)
		} else {
			fmt.Println("\n=== OpenAPI Specification ===")
			fmt.Println(string(specJSON))

			// Inspect the PaginationParams schema to verify optional fields
			if spec.Components != nil && spec.Components.Schemas != nil {
				if paginationSchema, ok := spec.Components.Schemas["PaginationParams"]; ok {
					fmt.Println("\n=== PaginationParams Required Fields ===")
					fmt.Printf("Required: %v\n", paginationSchema.Required)
					fmt.Println("\nExpected: All fields should be optional (empty required array)")
				}
			}

			// Check query parameters for the /users endpoint
			if spec.Paths != nil {
				if paths, ok := spec.Paths["/users"]; ok {
					if getOp := paths.Get; getOp != nil {
						fmt.Println("\n=== /users Query Parameters ===")
						for _, param := range getOp.Parameters {
							if param.In == "query" {
								fmt.Printf("- %s: required=%v\n", param.Name, param.Required)
							}
						}
						fmt.Println("\nExpected: All query parameters should have required=false")
					}
				}
			}
		}
	}

	// Start server
	fmt.Println("\n=== Starting Server ===")
	fmt.Println("Server running on http://localhost:8080")
	fmt.Println("\nTry these requests:")
	fmt.Println("  curl http://localhost:8080/users")
	fmt.Println("  curl 'http://localhost:8080/users?page=2&limit=20'")
	fmt.Println("  curl 'http://localhost:8080/users?search=john&sort_by=name&order=asc'")
	fmt.Println("  curl http://localhost:8080/users/cursor")
	fmt.Println("  curl 'http://localhost:8080/users/cursor?limit=5&cursor=eyJpZCI6IjEyMyIsInRzIjoxNjQwMDAwMDAwfQ=='")
	fmt.Println("\nOpenAPI spec available at: http://localhost:8080/openapi.json")
	fmt.Println("Swagger UI available at: http://localhost:8080/swagger")

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

