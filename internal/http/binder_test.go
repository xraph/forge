package http

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/rs/xid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/internal/shared"
)

// Test struct for basic binding.
type BasicBindRequest struct {
	TenantID string `path:"tenantId"`
	Name     string `query:"name"`
	APIKey   string `header:"X-API-Key"`
}

// Test struct for optional tag support.
type OptionalTagRequest struct {
	// Required by default (non-pointer, no optional tag)
	RequiredField string `query:"required"`
	// Explicitly optional via optional tag
	OptionalField string `optional:"true" query:"optional"`
	// Optional via omitempty
	OmitemptyField string `query:"omitempty,omitempty"`
	// Optional via pointer
	PointerField *string `query:"pointer"`
	// Optional with default
	DefaultField string `default:"default_value" optional:"true" query:"default"`
}

// Test struct for validation with optional fields.
type ValidationOptionalRequest struct {
	// Required field with validation
	Email string `format:"email" query:"email"`
	// Optional field with validation - should skip validation when empty
	OptionalEmail string `format:"email" optional:"true" query:"optionalEmail"`
	// Optional field with minLength - should skip when empty
	OptionalName string `minLength:"3" optional:"true" query:"optionalName"`
	// Optional field with pattern - should skip when empty
	OptionalCode string `optional:"true" pattern:"^[A-Z]{3}$" query:"optionalCode"`
	// Optional numeric with minimum - should skip when zero
	OptionalAge int `minimum:"18" optional:"true" query:"optionalAge"`
}

// Test struct for body field binding.
type BodyBindRequest struct {
	ID   string `path:"id"`
	Name string `json:"name"`
	Bio  string `json:"bio"  optional:"true"`
}

// Test struct for header binding with optional.
type HeaderBindRequest struct {
	Authorization string `header:"Authorization"`
	TraceID       string `header:"X-Trace-ID"             optional:"true"`
	RequestID     string `header:"X-Request-ID,omitempty"`
}

// Test struct with enum validation.
type EnumBindRequest struct {
	Status         string `enum:"active,inactive,pending" query:"status"`
	OptionalStatus string `enum:"active,inactive"         optional:"true" query:"optionalStatus"`
}

// Test struct with numeric validation.
type NumericBindRequest struct {
	Page         int `minimum:"1"   query:"page"`
	OptionalPage int `minimum:"1"   optional:"true" query:"optionalPage"`
	Limit        int `maximum:"100" query:"limit"`
	OptionalMax  int `maximum:"100" optional:"true" query:"optionalMax"`
}

// Test struct for tag precedence.
type PrecedenceRequest struct {
	// optional takes precedence over required
	Field1 string `optional:"true" query:"field1" required:"true"`
	// required takes precedence over default behavior
	Field2 string `query:"field2" required:"true"`
	// omitempty makes it optional
	Field3 string `query:"field3,omitempty"`
}

func TestBindRequest_BasicBinding(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users/123?name=john", nil)
	req.Header.Set("X-Api-Key", "secret-key")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("tenantId", "123")

	var bindReq BasicBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "123", bindReq.TenantID)
	assert.Equal(t, "john", bindReq.Name)
	assert.Equal(t, "secret-key", bindReq.APIKey)
}

func TestBindRequest_OptionalTag_NotRequired(t *testing.T) {
	// Test that optional fields don't cause validation errors when missing
	req := httptest.NewRequest(http.MethodGet, "/test?required=value", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq OptionalTagRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "value", bindReq.RequiredField)
	assert.Empty(t, bindReq.OptionalField)
	assert.Empty(t, bindReq.OmitemptyField)
	assert.Nil(t, bindReq.PointerField)
	assert.Equal(t, "default_value", bindReq.DefaultField)
}

func TestBindRequest_OptionalTag_ValidationSkipped(t *testing.T) {
	// Test that validation is skipped for empty optional fields
	req := httptest.NewRequest(http.MethodGet, "/test?email=test@example.com", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq ValidationOptionalRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "test@example.com", bindReq.Email)
	assert.Empty(t, bindReq.OptionalEmail)
	assert.Empty(t, bindReq.OptionalName)
	assert.Empty(t, bindReq.OptionalCode)
	assert.Zero(t, bindReq.OptionalAge)
}

func TestBindRequest_OptionalTag_ValidationAppliedWhenProvided(t *testing.T) {
	// Test that validation is applied when optional field has a value
	req := httptest.NewRequest(http.MethodGet, "/test?email=test@example.com&optionalEmail=invalid", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq ValidationOptionalRequest

	err := ctx.BindRequest(&bindReq)

	// Should fail validation because optionalEmail has invalid format
	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_OptionalTag_ValidOptionalValue(t *testing.T) {
	// Test that valid optional values pass validation
	req := httptest.NewRequest(http.MethodGet, "/test?email=test@example.com&optionalEmail=other@example.com&optionalName=John&optionalAge=25", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq ValidationOptionalRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "test@example.com", bindReq.Email)
	assert.Equal(t, "other@example.com", bindReq.OptionalEmail)
	assert.Equal(t, "John", bindReq.OptionalName)
	assert.Equal(t, 25, bindReq.OptionalAge)
}

func TestBindRequest_RequiredFieldMissing(t *testing.T) {
	// Test that required fields cause validation errors when missing
	req := httptest.NewRequest(http.MethodGet, "/test?optional=value", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq OptionalTagRequest

	err := ctx.BindRequest(&bindReq)

	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_HeaderOptionalTag(t *testing.T) {
	// Test that optional headers don't cause validation errors when missing
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer token")
	// Not setting X-Trace-ID or X-Request-ID
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq HeaderBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "Bearer token", bindReq.Authorization)
	assert.Empty(t, bindReq.TraceID)
	assert.Empty(t, bindReq.RequestID)
}

func TestBindRequest_HeaderRequiredMissing(t *testing.T) {
	// Test that required headers cause validation errors when missing
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	// Not setting Authorization header
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq HeaderBindRequest

	err := ctx.BindRequest(&bindReq)

	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_EnumOptional(t *testing.T) {
	// Test that optional enum fields don't fail when empty
	req := httptest.NewRequest(http.MethodGet, "/test?status=active", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq EnumBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "active", bindReq.Status)
	assert.Empty(t, bindReq.OptionalStatus)
}

func TestBindRequest_EnumOptional_InvalidWhenProvided(t *testing.T) {
	// Test that invalid optional enum values still fail validation
	req := httptest.NewRequest(http.MethodGet, "/test?status=active&optionalStatus=invalid", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq EnumBindRequest

	err := ctx.BindRequest(&bindReq)

	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_NumericOptional(t *testing.T) {
	// Test that optional numeric fields don't fail minimum validation when zero
	req := httptest.NewRequest(http.MethodGet, "/test?page=1&limit=50", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq NumericBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, 1, bindReq.Page)
	assert.Equal(t, 0, bindReq.OptionalPage) // Zero because not provided
	assert.Equal(t, 50, bindReq.Limit)
	assert.Equal(t, 0, bindReq.OptionalMax) // Zero because not provided
}

func TestBindRequest_NumericOptional_ValidatesWhenProvided(t *testing.T) {
	// Test that optional numeric fields validate when provided with invalid value
	req := httptest.NewRequest(http.MethodGet, "/test?page=1&limit=50&optionalPage=0", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq NumericBindRequest

	err := ctx.BindRequest(&bindReq)

	// optionalPage=0 should NOT fail because 0 is the zero value for optional int
	// and we skip validation for optional zero values
	require.NoError(t, err)
}

func TestBindRequest_Precedence_OptionalOverRequired(t *testing.T) {
	// Test that optional:"true" takes precedence over required:"true"
	req := httptest.NewRequest(http.MethodGet, "/test?field2=value", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq PrecedenceRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	// field1 should not be required (optional takes precedence)
	assert.Empty(t, bindReq.Field1)
	// field2 is required and provided
	assert.Equal(t, "value", bindReq.Field2)
	// field3 is optional (omitempty)
	assert.Empty(t, bindReq.Field3)
}

func TestBindRequest_Precedence_RequiredFailsWhenMissing(t *testing.T) {
	// Test that required:"true" field fails when missing
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq PrecedenceRequest

	err := ctx.BindRequest(&bindReq)

	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_BodyWithOptional(t *testing.T) {
	body := `{"name": "John"}`
	req := httptest.NewRequest(http.MethodPost, "/users/123", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("id", "123")

	var bindReq BodyBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "123", bindReq.ID)
	assert.Equal(t, "John", bindReq.Name)
	assert.Empty(t, bindReq.Bio) // Optional and not provided
}

func TestBindRequest_DefaultValues(t *testing.T) {
	// Test that default values are applied for optional fields
	req := httptest.NewRequest(http.MethodGet, "/test?required=value", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq OptionalTagRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "value", bindReq.RequiredField)
	assert.Equal(t, "default_value", bindReq.DefaultField)
}

func TestBindRequest_NilPointer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	err := ctx.BindRequest(nil)
	assert.Error(t, err)
}

func TestBindRequest_NonPointer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq BasicBindRequest

	err := ctx.BindRequest(bindReq) // passing value instead of pointer
	assert.Error(t, err)
}

// Helper functions tests

func TestIsBindFieldRequired_OptionalTag(t *testing.T) {
	type TestStruct struct {
		Optional string `optional:"true" query:"opt"`
		Required string `query:"req"     required:"true"`
		Default  string `query:"def"`
	}

	rt := reflect.TypeFor[TestStruct]()

	// optional:"true" field
	field, _ := rt.FieldByName("Optional")
	assert.False(t, isBindFieldRequired(field, field.Tag.Get("query")))

	// required:"true" field
	field, _ = rt.FieldByName("Required")
	assert.True(t, isBindFieldRequired(field, field.Tag.Get("query")))

	// default behavior (non-pointer = required)
	field, _ = rt.FieldByName("Default")
	assert.True(t, isBindFieldRequired(field, field.Tag.Get("query")))
}

func TestIsBindFieldRequired_Omitempty(t *testing.T) {
	type TestStruct struct {
		Omitempty string `query:"name,omitempty"`
		Normal    string `query:"normal"`
	}

	rt := reflect.TypeFor[TestStruct]()

	// omitempty field
	field, _ := rt.FieldByName("Omitempty")
	assert.False(t, isBindFieldRequired(field, field.Tag.Get("query")))

	// normal field
	field, _ = rt.FieldByName("Normal")
	assert.True(t, isBindFieldRequired(field, field.Tag.Get("query")))
}

func TestIsBindFieldRequired_Pointer(t *testing.T) {
	type TestStruct struct {
		Pointer *string `query:"ptr"`
		Value   string  `query:"val"`
	}

	rt := reflect.TypeFor[TestStruct]()

	// pointer field
	field, _ := rt.FieldByName("Pointer")
	assert.False(t, isBindFieldRequired(field, field.Tag.Get("query")))

	// value field
	field, _ = rt.FieldByName("Value")
	assert.True(t, isBindFieldRequired(field, field.Tag.Get("query")))
}

func TestIsValidationFieldRequired(t *testing.T) {
	type TestStruct struct {
		Optional     string `json:"opt"                    optional:"true"`
		Required     string `json:"req"                    required:"true"`
		JsonOmit     string `json:"jsonOmit,omitempty"`
		QueryOmit    string `query:"queryOmit,omitempty"`
		HeaderOmit   string `header:"headerOmit,omitempty"`
		BodyOmit     string `body:"bodyOmit,omitempty"`
		DefaultField string `json:"default"`
	}

	rt := reflect.TypeFor[TestStruct]()

	tests := []struct {
		name     string
		expected bool
	}{
		{"Optional", false},
		{"Required", true},
		{"JsonOmit", false},
		{"QueryOmit", false},
		{"HeaderOmit", false},
		{"BodyOmit", false},
		{"DefaultField", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, _ := rt.FieldByName(tt.name)
			assert.Equal(t, tt.expected, isValidationFieldRequired(field))
		})
	}
}

// Integration tests with embedded structs

type EmbeddedOptionalRequest struct {
	BaseParams

	Name string `query:"name"`
}

type BaseParams struct {
	Limit  int    `default:"10"    optional:"true" query:"limit"`
	Offset int    `default:"0"     optional:"true" query:"offset"`
	Sort   string `optional:"true" query:"sort"`
}

func TestBindRequest_EmbeddedOptional(t *testing.T) {
	// Test that embedded struct fields are properly bound with optional tags
	req := httptest.NewRequest(http.MethodGet, "/test?name=John&limit=20", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq EmbeddedOptionalRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "John", bindReq.Name)
	// Embedded fields should be bound when their tags match query params
	assert.Equal(t, 20, bindReq.Limit)
	// Optional fields with defaults should have defaults applied
	assert.Equal(t, 0, bindReq.Offset) // default:"0"
	assert.Empty(t, bindReq.Sort)      // optional, no value provided
}

func TestBindRequest_EmbeddedOptional_WithDefaults(t *testing.T) {
	// Test that defaults are applied to embedded struct fields
	req := httptest.NewRequest(http.MethodGet, "/test?name=Jane", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq EmbeddedOptionalRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "Jane", bindReq.Name)
	// Embedded optional fields should use defaults when not provided
	assert.Equal(t, 10, bindReq.Limit) // default:"10"
	assert.Equal(t, 0, bindReq.Offset) // default:"0"
	assert.Empty(t, bindReq.Sort)      // optional, no default
}

func TestBindRequest_EmbeddedOptional_NoRequired(t *testing.T) {
	// Test that embedded optional fields don't cause errors when not provided
	req := httptest.NewRequest(http.MethodGet, "/test?name=Test", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq EmbeddedOptionalRequest

	err := ctx.BindRequest(&bindReq)
	// Should not error - all embedded fields are optional
	require.NoError(t, err)

	assert.Equal(t, "Test", bindReq.Name)
}

// Test struct for TextUnmarshaler support (e.g., xid.ID).
type TextUnmarshalerRequest struct {
	WorkspaceID xid.ID `path:"workspaceId"`
	UserID      xid.ID `query:"userId"`
}

// Test struct for optional TextUnmarshaler.
type OptionalTextUnmarshalerRequest struct {
	WorkspaceID xid.ID  `path:"workspaceId"`
	TraceID     *xid.ID `optional:"true"    query:"traceId"`
}

func TestBindRequest_TextUnmarshaler_XID(t *testing.T) {
	// Generate a valid XID for testing
	validID := xid.New()
	validIDStr := validID.String()

	req := httptest.NewRequest(http.MethodGet, "/workspaces/"+validIDStr+"?userId="+validIDStr, nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("workspaceId", validIDStr)

	var bindReq TextUnmarshalerRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, validID, bindReq.WorkspaceID)
	assert.Equal(t, validID, bindReq.UserID)
}

func TestBindRequest_TextUnmarshaler_InvalidXID(t *testing.T) {
	// Test with invalid XID string
	req := httptest.NewRequest(http.MethodGet, "/workspaces/invalid-xid?userId=also-invalid", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("workspaceId", "invalid-xid")

	var bindReq TextUnmarshalerRequest

	err := ctx.BindRequest(&bindReq)

	// Should return validation errors
	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_TextUnmarshaler_OptionalXID(t *testing.T) {
	// Test with optional XID field not provided
	validID := xid.New()
	validIDStr := validID.String()

	req := httptest.NewRequest(http.MethodGet, "/workspaces/"+validIDStr, nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("workspaceId", validIDStr)

	var bindReq OptionalTextUnmarshalerRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, validID, bindReq.WorkspaceID)
	assert.Nil(t, bindReq.TraceID) // Optional and not provided
}

func TestBindRequest_TextUnmarshaler_OptionalXIDProvided(t *testing.T) {
	// Test with optional XID field provided
	workspaceID := xid.New()
	traceID := xid.New()

	req := httptest.NewRequest(http.MethodGet, "/workspaces/"+workspaceID.String()+"?traceId="+traceID.String(), nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("workspaceId", workspaceID.String())

	var bindReq OptionalTextUnmarshalerRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, workspaceID, bindReq.WorkspaceID)
	require.NotNil(t, bindReq.TraceID)
	assert.Equal(t, traceID, *bindReq.TraceID)
}

// CustomID is a test type that implements encoding.TextUnmarshaler.
type CustomID string

func (c *CustomID) UnmarshalText(text []byte) error {
	*c = CustomID("custom:" + string(text))

	return nil
}

type CustomIDRequest struct {
	ID CustomID `path:"id"`
}

func TestBindRequest_TextUnmarshaler_CustomType(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/items/123", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)
	ctx.setParam("id", "123")

	var bindReq CustomIDRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, CustomID("custom:123"), bindReq.ID)
}

// Test struct for boolean query params.
type BooleanBindRequest struct {
	IncludeTemplate bool `query:"includeTemplate" required:"true"`
	DryRun          bool `query:"dryRun"`
	Verbose         bool `optional:"true"         query:"verbose"`
}

func TestBindRequest_BooleanFalseRequired(t *testing.T) {
	// Test that required boolean fields with explicit false value don't fail validation
	req := httptest.NewRequest(http.MethodGet, "/test?includeTemplate=false&dryRun=true", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq BooleanBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.False(t, bindReq.IncludeTemplate) // Explicitly set to false
	assert.True(t, bindReq.DryRun)
	assert.False(t, bindReq.Verbose) // Not provided, defaults to false
}

func TestBindRequest_BooleanTrueRequired(t *testing.T) {
	// Test that required boolean fields with explicit true value pass validation
	req := httptest.NewRequest(http.MethodGet, "/test?includeTemplate=true&dryRun=false", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq BooleanBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.True(t, bindReq.IncludeTemplate)
	assert.False(t, bindReq.DryRun) // Explicitly set to false
}

func TestBindRequest_BooleanMissingRequired(t *testing.T) {
	// Test that required boolean fields fail validation when not provided
	req := httptest.NewRequest(http.MethodGet, "/test?dryRun=true", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq BooleanBindRequest

	err := ctx.BindRequest(&bindReq)

	// Should fail because includeTemplate is required but not provided
	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_BooleanOptional(t *testing.T) {
	// Test that optional boolean fields don't fail validation when not provided
	req := httptest.NewRequest(http.MethodGet, "/test?includeTemplate=true&dryRun=false", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq BooleanBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.True(t, bindReq.IncludeTemplate)
	assert.False(t, bindReq.DryRun)
	assert.False(t, bindReq.Verbose) // Optional and not provided
}

// Test struct for numeric query params with zero values.
type NumericZeroBindRequest struct {
	Count  int     `query:"count"  required:"true"`
	Limit  int     `query:"limit"  required:"true"`
	Price  float64 `query:"price"  required:"true"`
	Offset int     `query:"offset"`
}

func TestBindRequest_NumericZeroRequired(t *testing.T) {
	// Test that required numeric fields with explicit 0 value don't fail validation
	req := httptest.NewRequest(http.MethodGet, "/test?count=0&limit=0&price=0.0&offset=5", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq NumericZeroBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, 0, bindReq.Count)   // Explicitly set to 0
	assert.Equal(t, 0, bindReq.Limit)   // Explicitly set to 0
	assert.Equal(t, 0.0, bindReq.Price) // Explicitly set to 0.0
	assert.Equal(t, 5, bindReq.Offset)
}

func TestBindRequest_NumericPositiveRequired(t *testing.T) {
	// Test that required numeric fields with positive values pass validation
	req := httptest.NewRequest(http.MethodGet, "/test?count=10&limit=20&price=99.99&offset=0", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq NumericZeroBindRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, 10, bindReq.Count)
	assert.Equal(t, 20, bindReq.Limit)
	assert.Equal(t, 99.99, bindReq.Price)
	assert.Equal(t, 0, bindReq.Offset) // Explicitly set to 0
}

func TestBindRequest_NumericMissingRequired(t *testing.T) {
	// Test that required numeric fields fail validation when not provided
	req := httptest.NewRequest(http.MethodGet, "/test?offset=5", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq NumericZeroBindRequest

	err := ctx.BindRequest(&bindReq)

	// Should fail because count, limit, and price are required but not provided
	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

// Test struct for string query params with empty values.
type StringEmptyBindRequest struct {
	Name        string `query:"name"        required:"true"`
	Description string `query:"description" required:"true"`
	Tag         string `query:"tag"`
}

func TestBindRequest_StringEmptyRequired(t *testing.T) {
	// Test that required string fields with explicit empty value fail validation
	// (empty string is legitimately ambiguous and should fail)
	req := httptest.NewRequest(http.MethodGet, "/test?name=&description=test&tag=", nil)
	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq StringEmptyBindRequest

	err := ctx.BindRequest(&bindReq)

	// Should fail because name is required and empty
	require.Error(t, err)
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

// Test struct for JSON body with boolean fields.
type JsonBodyBooleanRequest struct {
	IsPrivate bool  `json:"isPrivate"`
	IsActive  bool  `json:"isActive"            required:"true"`
	IsEnabled *bool `json:"isEnabled,omitempty"`
}

func TestBindRequest_JsonBodyBooleanFalse(t *testing.T) {
	// Test that JSON body fields with explicit false value don't fail validation
	body := bytes.NewBufferString(`{"isPrivate": false, "isActive": false}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyBooleanRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.False(t, bindReq.IsPrivate) // Explicitly set to false
	assert.False(t, bindReq.IsActive)  // Explicitly set to false
	assert.Nil(t, bindReq.IsEnabled)   // Not provided
}

func TestBindRequest_JsonBodyBooleanTrue(t *testing.T) {
	// Test that JSON body fields with explicit true value pass validation
	falseVal := false
	body := bytes.NewBufferString(`{"isPrivate": true, "isActive": true, "isEnabled": false}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyBooleanRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.True(t, bindReq.IsPrivate) // Explicitly set to true
	assert.True(t, bindReq.IsActive)  // Explicitly set to true
	assert.NotNil(t, bindReq.IsEnabled)
	assert.Equal(t, &falseVal, bindReq.IsEnabled) // Explicitly set to false via pointer
}

func TestBindRequest_JsonBodyBooleanRequired(t *testing.T) {
	// Test that required boolean fields work correctly
	// Note: After unmarshaling, we cannot distinguish between missing field and explicit false
	// Therefore, required validation for non-pointer boolean fields is not meaningful
	body := bytes.NewBufferString(`{"isPrivate": true}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyBooleanRequest

	err := ctx.BindRequest(&bindReq)
	// Should not error - isActive defaults to false (cannot detect if missing)
	require.NoError(t, err)
	assert.True(t, bindReq.IsPrivate)
	assert.False(t, bindReq.IsActive)
}

func TestBindRequest_JsonBodyBooleanOptional(t *testing.T) {
	// Test that optional boolean fields (pointers) work correctly
	trueVal := true
	body := bytes.NewBufferString(`{"isPrivate": false, "isActive": true, "isEnabled": true}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyBooleanRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.False(t, bindReq.IsPrivate)
	assert.True(t, bindReq.IsActive)
	assert.NotNil(t, bindReq.IsEnabled)
	assert.Equal(t, &trueVal, bindReq.IsEnabled)
}

// Test struct for JSON body with numeric zero values.
type JsonBodyNumericRequest struct {
	Count  int     `json:"count"`
	Offset int     `json:"offset"          required:"true"`
	Price  float64 `json:"price"`
	Limit  *int    `json:"limit,omitempty"`
}

func TestBindRequest_JsonBodyNumericZero(t *testing.T) {
	// Test that JSON body fields with explicit 0 value don't fail validation
	body := bytes.NewBufferString(`{"count": 0, "offset": 0, "price": 0.0}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyNumericRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, 0, bindReq.Count)   // Explicitly set to 0
	assert.Equal(t, 0, bindReq.Offset)  // Explicitly set to 0
	assert.Equal(t, 0.0, bindReq.Price) // Explicitly set to 0.0
	assert.Nil(t, bindReq.Limit)        // Not provided
}

func TestBindRequest_JsonBodyNumericPositive(t *testing.T) {
	// Test that JSON body fields with positive values pass validation
	limitVal := 100
	body := bytes.NewBufferString(`{"count": 10, "offset": 5, "price": 99.99, "limit": 100}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyNumericRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, 10, bindReq.Count)
	assert.Equal(t, 5, bindReq.Offset)
	assert.Equal(t, 99.99, bindReq.Price)
	assert.NotNil(t, bindReq.Limit)
	assert.Equal(t, &limitVal, bindReq.Limit)
}

func TestBindRequest_JsonBodyNumericRequired(t *testing.T) {
	// Test that required numeric fields work correctly
	// Note: After unmarshaling, we cannot distinguish between missing field and explicit 0
	// Therefore, required validation for non-pointer numeric fields is not meaningful
	body := bytes.NewBufferString(`{"count": 5, "price": 10.5}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyNumericRequest

	err := ctx.BindRequest(&bindReq)
	// Should not error - offset defaults to 0 (cannot detect if missing)
	require.NoError(t, err)
	assert.Equal(t, 5, bindReq.Count)
	assert.Equal(t, 0, bindReq.Offset)
	assert.Equal(t, 10.5, bindReq.Price)
}

func TestBindRequest_JsonBodyNumericOptional(t *testing.T) {
	// Test that optional numeric fields (pointers) work correctly
	limitVal := 50
	body := bytes.NewBufferString(`{"count": 0, "offset": 0, "price": 0, "limit": 50}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyNumericRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, 0, bindReq.Count)
	assert.Equal(t, 0, bindReq.Offset)
	assert.Equal(t, 0.0, bindReq.Price)
	assert.NotNil(t, bindReq.Limit)
	assert.Equal(t, &limitVal, bindReq.Limit)
}

// Test struct for mixed body with boolean and other fields.
type JsonBodyMixedRequest struct {
	Name      string  `json:"name"      maxLength:"100" minLength:"1"`
	IsPrivate bool    `json:"isPrivate"`
	Count     int     `json:"count"`
	Price     float64 `json:"price"`
}

func TestBindRequest_JsonBodyMixedZeroValues(t *testing.T) {
	// Test that mixed JSON body with various zero values works correctly
	body := bytes.NewBufferString(`{"name": "test", "isPrivate": false, "count": 0, "price": 0.0}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq JsonBodyMixedRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err)

	assert.Equal(t, "test", bindReq.Name)
	assert.False(t, bindReq.IsPrivate)  // Explicit false
	assert.Equal(t, 0, bindReq.Count)   // Explicit 0
	assert.Equal(t, 0.0, bindReq.Price) // Explicit 0.0
}

// Test struct for all primitive numeric types.
type AllPrimitiveTypesRequest struct {
	// Signed integers
	Int8Field  int8  `json:"int8Field"`
	Int16Field int16 `json:"int16Field"`
	Int32Field int32 `json:"int32Field"`
	Int64Field int64 `json:"int64Field"`
	IntField   int   `json:"intField"`

	// Unsigned integers
	Uint8Field  uint8  `json:"uint8Field"`
	Uint16Field uint16 `json:"uint16Field"`
	Uint32Field uint32 `json:"uint32Field"`
	Uint64Field uint64 `json:"uint64Field"`
	UintField   uint   `json:"uintField"`

	// Floats
	Float32Field float32 `json:"float32Field"`
	Float64Field float64 `json:"float64Field"`

	// Boolean
	BoolField bool `json:"boolField"`

	// String
	StringField string `json:"stringField"`
}

func TestBindRequest_AllPrimitiveTypesZeroValues(t *testing.T) {
	// Test that ALL primitive numeric types accept zero values
	body := bytes.NewBufferString(`{
		"int8Field": 0,
		"int16Field": 0,
		"int32Field": 0,
		"int64Field": 0,
		"intField": 0,
		"uint8Field": 0,
		"uint16Field": 0,
		"uint32Field": 0,
		"uint64Field": 0,
		"uintField": 0,
		"float32Field": 0.0,
		"float64Field": 0.0,
		"boolField": false,
		"stringField": "valid"
	}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq AllPrimitiveTypesRequest

	err := ctx.BindRequest(&bindReq)
	require.NoError(t, err, "All primitive types with zero values should pass validation")

	// Verify all zero values were correctly bound
	assert.Equal(t, int8(0), bindReq.Int8Field)
	assert.Equal(t, int16(0), bindReq.Int16Field)
	assert.Equal(t, int32(0), bindReq.Int32Field)
	assert.Equal(t, int64(0), bindReq.Int64Field)
	assert.Equal(t, 0, bindReq.IntField)

	assert.Equal(t, uint8(0), bindReq.Uint8Field)
	assert.Equal(t, uint16(0), bindReq.Uint16Field)
	assert.Equal(t, uint32(0), bindReq.Uint32Field)
	assert.Equal(t, uint64(0), bindReq.Uint64Field)
	assert.Equal(t, uint(0), bindReq.UintField)

	assert.Equal(t, float32(0.0), bindReq.Float32Field)
	assert.Equal(t, float64(0.0), bindReq.Float64Field)

	assert.False(t, bindReq.BoolField)
	assert.Equal(t, "valid", bindReq.StringField)
}

// Test struct for required empty string validation.
type RequiredStringRequest struct {
	Name  string `json:"name"  required:"true"`
	Email string `json:"email" required:"true"`
}

func TestBindRequest_JsonBodyEmptyStringRequired(t *testing.T) {
	// Test that required string fields with empty values fail validation
	body := bytes.NewBufferString(`{"name": "", "email": "test@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq RequiredStringRequest

	err := ctx.BindRequest(&bindReq)

	// Should fail because name is empty string
	require.Error(t, err, "Empty string should fail validation for required field")
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}

func TestBindRequest_JsonBodyEmptyStringBothRequired(t *testing.T) {
	// Test that multiple required string fields with empty values fail validation
	body := bytes.NewBufferString(`{"name": "", "email": ""}`)
	req := httptest.NewRequest(http.MethodPost, "/test", body)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()

	ctx := NewContext(rec, req, nil).(*Ctx)

	var bindReq RequiredStringRequest

	err := ctx.BindRequest(&bindReq)

	// Should fail because both fields are empty strings
	require.Error(t, err, "Empty strings should fail validation for required fields")
	valErrors := &shared.ValidationErrors{}
	ok := errors.As(err, &valErrors)
	require.True(t, ok)
	assert.True(t, valErrors.HasErrors())
}
