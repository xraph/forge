package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/xraph/forge"
)

type TestRequest struct {
	IncludeTemplate bool `query:"includeTemplate" required:"true" description:"Include template in response"`
	DryRun          bool `query:"dryRun" description:"Perform a dry run"`
}

func main() {
	app := forge.New()

	app.GET("/test", func(ctx forge.Context) error {
		var req TestRequest
		if err := ctx.BindRequest(&req); err != nil {
			return ctx.Status(http.StatusBadRequest).JSON(map[string]any{
				"error": err.Error(),
			})
		}

		return ctx.JSON(map[string]any{
			"includeTemplate": req.IncludeTemplate,
			"dryRun":          req.DryRun,
		})
	})

	// Test 1: includeTemplate=false (should work)
	req1 := httptest.NewRequest(http.MethodGet, "/test?includeTemplate=false&dryRun=true", nil)
	rec1 := httptest.NewRecorder()
	app.ServeHTTP(rec1, req1)
	fmt.Printf("Test 1 (includeTemplate=false): Status=%d Body=%s\n", rec1.Code, rec1.Body.String())

	// Test 2: includeTemplate=true (should work)
	req2 := httptest.NewRequest(http.MethodGet, "/test?includeTemplate=true&dryRun=false", nil)
	rec2 := httptest.NewRecorder()
	app.ServeHTTP(rec2, req2)
	fmt.Printf("Test 2 (includeTemplate=true): Status=%d Body=%s\n", rec2.Code, rec2.Body.String())

	// Test 3: missing includeTemplate (should fail)
	req3 := httptest.NewRequest(http.MethodGet, "/test?dryRun=true", nil)
	rec3 := httptest.NewRecorder()
	app.ServeHTTP(rec3, req3)
	fmt.Printf("Test 3 (missing includeTemplate): Status=%d Body=%s\n", rec3.Code, rec3.Body.String())
}

