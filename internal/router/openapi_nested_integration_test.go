package router

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/xraph/forge/internal/shared"
)

// Integration test for nested struct component generation in full OpenAPI spec.
func TestOpenAPIGenerator_NestedStructComponents(t *testing.T) {
	// Define nested struct types
	type testAddress struct {
		Street  string `description:"Street address" json:"street"`
		City    string `description:"City name"      json:"city"`
		ZipCode string `description:"Postal code"    json:"zipCode"`
	}

	type testMetadata struct {
		CreatedAt time.Time         `description:"Creation timestamp" json:"created_at"`
		Tags      []string          `description:"Tags"               json:"tags,omitempty"`
		Custom    map[string]string `description:"Custom fields"      json:"custom,omitempty"`
	}

	type testAuthFactor struct {
		FactorID int          `description:"Factor ID"   json:"factor_id"`
		Type     string       `description:"Factor type" json:"type"`
		Metadata testMetadata `description:"Metadata"    json:"metadata,omitempty"`
	}

	type testChallengeResponse struct {
		ChallengeID      int              `description:"Challenge ID"      json:"challenge_id"`
		SessionID        string           `description:"Session ID"        json:"session_id"`
		AvailableFactors []testAuthFactor `description:"Available factors" json:"available_factors"`
		ExpiresAt        time.Time        `description:"Expiration time"   json:"expires_at"`
	}

	type testUserProfile struct {
		UserID  string           `description:"User ID"         json:"user_id"`
		Address testAddress      `description:"Primary address" json:"address"`
		Factors []testAuthFactor `description:"Auth factors"    json:"factors,omitempty"`
	}

	// Create router with OpenAPI enabled
	router := NewRouter(
		WithOpenAPI(OpenAPIConfig{
			Title:       "Nested Struct Test",
			Description: "Testing nested struct component generation",
			Version:     "1.0.0",
		}),
	)

	// Empty request types for GET endpoints
	type emptyRequest struct{}

	// Add routes with nested struct responses
	err1 := router.GET("/challenge", func(ctx shared.Context, req *emptyRequest) (*testChallengeResponse, error) {
		return &testChallengeResponse{}, nil
	}, WithResponseSchema(200, "Challenge", &testChallengeResponse{}))
	if err1 != nil {
		t.Fatalf("Failed to add /challenge route: %v", err1)
	}

	err2 := router.GET("/profile", func(ctx shared.Context, req *emptyRequest) (*testUserProfile, error) {
		return &testUserProfile{}, nil
	}, WithResponseSchema(200, "Profile", &testUserProfile{}))
	if err2 != nil {
		t.Fatalf("Failed to add /profile route: %v", err2)
	}

	t.Logf("Routes added successfully")

	// Generate spec
	spec := router.OpenAPISpec()

	if spec == nil {
		t.Fatal("Expected OpenAPI spec to be generated")
	}

	if spec.Components == nil || spec.Components.Schemas == nil {
		t.Fatal("Expected components.schemas to exist")
	}

	// Debug: Print what components were actually registered
	t.Logf("Components registered: %d", len(spec.Components.Schemas))

	for name := range spec.Components.Schemas {
		t.Logf("  - %s", name)
	}

	// Debug: Print paths
	t.Logf("Paths registered: %d", len(spec.Paths))

	for path := range spec.Paths {
		t.Logf("  - %s", path)
	}

	// Verify all nested types are registered as components
	expectedComponents := []string{
		"testChallengeResponse",
		"testAuthFactor",
		"testMetadata",
		"testAddress",
		"testUserProfile",
	}

	for _, compName := range expectedComponents {
		if _, ok := spec.Components.Schemas[compName]; !ok {
			t.Errorf("Expected component '%s' to be registered", compName)
		}
	}

	// Verify ChallengeResponse structure
	challengeSchema, ok := spec.Components.Schemas["testChallengeResponse"]
	if !ok {
		t.Fatal("testChallengeResponse component not found")
	}

	availableFactorsField, ok := challengeSchema.Properties["available_factors"]
	if !ok {
		t.Fatal("available_factors field not found in testChallengeResponse")
	}

	if availableFactorsField.Type != "array" {
		t.Errorf("Expected available_factors type 'array', got %s", availableFactorsField.Type)
	}

	if availableFactorsField.Items == nil {
		t.Fatal("Expected available_factors to have items schema")
	}

	if availableFactorsField.Items.Ref != "#/components/schemas/testAuthFactor" {
		t.Errorf("Expected available_factors items ref to testAuthFactor, got %s", availableFactorsField.Items.Ref)
	}

	// Verify AuthFactor has metadata ref (deeply nested)
	authFactorSchema, ok := spec.Components.Schemas["testAuthFactor"]
	if !ok {
		t.Fatal("testAuthFactor component not found")
	}

	metadataField, ok := authFactorSchema.Properties["metadata"]
	if !ok {
		t.Fatal("metadata field not found in testAuthFactor")
	}

	if metadataField.Ref != "#/components/schemas/testMetadata" {
		t.Errorf("Expected metadata ref to testMetadata, got %s", metadataField.Ref)
	}

	// Verify factor_id field exists with correct type
	factorIDField, ok := authFactorSchema.Properties["factor_id"]
	if !ok {
		t.Fatal("factor_id field not found in testAuthFactor")
	}

	if factorIDField.Type != "integer" {
		t.Errorf("Expected factor_id type 'integer', got %s", factorIDField.Type)
	}

	// Print spec for manual inspection (optional)
	if testing.Verbose() {
		specJSON, _ := json.MarshalIndent(spec, "", "  ")
		t.Logf("Generated OpenAPI Spec:\n%s", specJSON)
	}

	t.Logf("✓ Successfully verified nested struct component generation")
	t.Logf("✓ Components registered: %v", expectedComponents)
}
