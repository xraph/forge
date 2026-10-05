package typescript

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xraph/forge/internal/client"
)

func jsonMediaEndpoint(responseType, bodyType string) *client.Endpoint {
	schemaRef := &client.Schema{Ref: "#/components/schemas/Order"}

	ep := &client.Endpoint{
		Method: "POST", Path: "/orders", OperationID: "orders.create",
		Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{responseType: {Schema: schemaRef}}}},
	}

	if bodyType != "" {
		ep.RequestBody = &client.RequestBody{Required: true, Content: map[string]*client.MediaType{bodyType: {Schema: schemaRef}}}
	}

	return ep
}

func TestIsJSONMediaType(t *testing.T) {
	for contentType, want := range map[string]bool{
		"application/json":                  true,
		"Application/JSON; charset=utf-8":   true,
		" text/json ":                       true,
		"application/problem+json":          true,
		"application/vnd.api+json; foo=bar": true,
		"application/jsonl":                 false,
		"application/x-ndjson":              false,
		"text/plain":                        false,
		"application/json-patch":            false,
		"image/png":                         false,
		"":                                  false,
		"application/xml; note=\"+json\"":   false,
	} {
		assert.Equal(t, want, isJSONMediaType(contentType), contentType)
	}
}

// A +json response is JSON on the wire, so it gets the codec an
// application/json response with the same schema gets.
func TestJSONSuffixResponseGetsItsCodec(t *testing.T) {
	for _, contentType := range []string{"application/json", "application/problem+json", "application/vnd.api+json"} {
		id, warning := responseCodecRef(jsonMediaEndpoint(contentType, ""))

		assert.Equal(t, "Order", id, contentType)
		assert.Empty(t, warning, contentType)
	}

	id, _ := responseCodecRef(jsonMediaEndpoint("image/png", ""))
	assert.Empty(t, id, "a non-JSON response never gets a codec")
}

func TestJSONSuffixRequestBodyIsPlannedAsJSONWithItsCodec(t *testing.T) {
	ep := jsonMediaEndpoint("application/json", "application/vnd.api+json")

	assert.Equal(t, "application/vnd.api+json", requestBodyContentType(ep))

	id, warning := requestBodyCodecRef(ep)
	assert.Equal(t, "Order", id)
	assert.Empty(t, warning)
}

func TestJSONEntryOutranksTextAndOtherBodies(t *testing.T) {
	schema := &client.Schema{Ref: "#/components/schemas/Order"}
	ep := &client.Endpoint{RequestBody: &client.RequestBody{Content: map[string]*client.MediaType{
		"text/plain":               {Schema: &client.Schema{Type: "string"}},
		"multipart/form-data":      {Schema: &client.Schema{Type: "object"}},
		"application/vnd.api+json": {Schema: schema},
		"application/json":         {},
	}}}

	// A schemaless application/json is skipped, as it always was, so the
	// +json entry that has a schema wins.
	assert.Equal(t, "application/vnd.api+json", requestBodyContentType(ep))

	ep.RequestBody.Content["application/json"] = &client.MediaType{Schema: schema}
	assert.Equal(t, "application/json", requestBodyContentType(ep))
}

func TestJSONSuffixArrayResponseRegistersItsCodec(t *testing.T) {
	spec := &client.APISpec{
		Info:    client.APIInfo{Title: "T", Version: "1"},
		Schemas: map[string]*client.Schema{"Order": {Type: "object", Properties: map[string]*client.Schema{"order_no": {Type: "string"}}}},
		Endpoints: []client.Endpoint{{
			Method: "GET", Path: "/orders", OperationID: "orders.list",
			Responses: map[int]*client.Response{200: {Content: map[string]*client.MediaType{
				"application/vnd.api+json": {Schema: &client.Schema{Type: "array", Items: &client.Schema{Ref: "#/components/schemas/Order"}}},
			}}},
		}},
	}

	cfg := client.DefaultConfig()
	cfg.Language = "typescript"
	table := buildCodecTable(spec, cfg)

	id, _ := responseCodecRef(&spec.Endpoints[0])
	assert.Equal(t, arrayRefCodecID("Order"), id)
	assert.Contains(t, table.entries, id, "the array codec the response names must be in the table")
}
