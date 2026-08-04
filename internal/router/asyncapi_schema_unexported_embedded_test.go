package router

import (
	"maps"
	"slices"
	"testing"
)

// The AsyncAPI generator walks struct fields independently of the OpenAPI one,
// so it needs its own coverage for embedded fields whose type name is lowercase.
// unexportedItemBase is declared in openapi_schema_unexported_embedded_test.go.

type traceHeaderBase struct {
	TraceID string `header:"X-Trace-Id" required:"true"`
}

type ExportedSpanHeaders struct {
	SpanID string `header:"X-Span-Id"`
}

// midHeaderBase is unexported and itself embeds a header-carrying struct,
// exercising the recursion in flattenEmbeddedHeaders.
type midHeaderBase struct {
	ExportedSpanHeaders

	CorrelationID string `header:"X-Correlation-Id"`
}

type EventWithUnexportedHeaderBase struct {
	traceHeaderBase

	OrderID string `json:"order_id"`
}

type EventWithNestedUnexportedHeaderBase struct {
	midHeaderBase

	OrderID string `json:"order_id"`
}

// EventWithUnexportedPayloadBase mixes a header field with an embedded
// unexported-named payload struct, so SplitMessageComponents takes its
// header-aware path instead of delegating wholesale to the OpenAPI generator.
type EventWithUnexportedPayloadBase struct {
	unexportedItemBase

	TraceID string `header:"X-Trace-Id"`
	Name    string `json:"name"`
}

func newTestAsyncAPISchemaGenerator() *asyncAPISchemaGenerator {
	return newAsyncAPISchemaGenerator(make(map[string]*Schema), nil)
}

func TestAsyncAPIHeadersPromoteEmbeddedUnexportedNamedStruct(t *testing.T) {
	schema := newTestAsyncAPISchemaGenerator().GenerateHeadersSchema(EventWithUnexportedHeaderBase{})
	if schema == nil {
		t.Fatal("GenerateHeadersSchema returned nil; X-Trace-Id was not promoted")
	}

	if _, ok := schema.Properties["X-Trace-Id"]; !ok {
		t.Errorf("header properties = %v, want X-Trace-Id", slices.Sorted(maps.Keys(schema.Properties)))
	}

	if !slices.Contains(schema.Required, "X-Trace-Id") {
		t.Errorf("required = %v, want it to contain X-Trace-Id", schema.Required)
	}
}

func TestAsyncAPIHeadersPromoteNestedEmbeddedUnexportedNamedStruct(t *testing.T) {
	schema := newTestAsyncAPISchemaGenerator().GenerateHeadersSchema(EventWithNestedUnexportedHeaderBase{})
	if schema == nil {
		t.Fatal("GenerateHeadersSchema returned nil; nested headers were not promoted")
	}

	got := slices.Sorted(maps.Keys(schema.Properties))

	want := []string{"X-Correlation-Id", "X-Span-Id"}
	if !slices.Equal(got, want) {
		t.Errorf("header properties = %v, want %v", got, want)
	}
}

func TestAsyncAPIPayloadPromotesEmbeddedUnexportedNamedStruct(t *testing.T) {
	headers, payload := newTestAsyncAPISchemaGenerator().SplitMessageComponents(EventWithUnexportedPayloadBase{})

	if headers == nil {
		t.Fatal("expected a headers schema for the X-Trace-Id field")
	}

	if payload == nil {
		t.Fatal("SplitMessageComponents returned no payload schema")
	}

	got := slices.Sorted(maps.Keys(payload.Properties))

	want := []string{"item_id", "name"}
	if !slices.Equal(got, want) {
		t.Errorf("payload properties = %v, want %v", got, want)
	}
}
