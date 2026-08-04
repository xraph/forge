package router

import (
	"maps"
	"slices"
	"testing"
)

// The query/header parameter walkers and the unified request/response
// component extractors each walk struct fields independently of
// generateStructSchema, so each needs its own coverage for fields embedded
// from a lowercase-named type. See skipStructField for the underlying rule.

type unexportedQueryBase struct {
	Page int `query:"page"`
}

type ExportedSortQuery struct {
	Sort string `query:"sort"`
}

// midQueryBase is unexported and itself embeds a query-carrying struct,
// exercising the recursion in flattenEmbeddedQueryParams.
type midQueryBase struct {
	ExportedSortQuery

	Limit int `query:"limit"`
}

type unexportedHeaderParamBase struct {
	Tenant string `header:"X-Tenant"`
}

type ExportedRegionHeader struct {
	Region string `header:"X-Region"`
}

// midHeaderParamBase exercises the recursion in flattenEmbeddedHeaderParams.
type midHeaderParamBase struct {
	ExportedRegionHeader

	Actor string `header:"X-Actor"`
}

type ListWithUnexportedQueryBase struct {
	unexportedQueryBase

	Search string `query:"search"`
}

type ListWithNestedUnexportedQueryBase struct {
	midQueryBase

	Search string `query:"search"`
}

type ListWithUnexportedHeaderBase struct {
	unexportedHeaderParamBase

	Search string `query:"search"`
}

type ListWithNestedUnexportedHeaderBase struct {
	midHeaderParamBase

	Search string `query:"search"`
}

// unexportedBodyBase carries the JSON body field that must survive promotion
// through the unified request/response extractors.
type unexportedBodyBase struct {
	ItemID string `json:"item_id"`
}

// RequestWithUnexportedBase mixes a path param with an embedded unexported-named
// body struct, so extractUnifiedRequestComponents takes its tag-aware path.
type RequestWithUnexportedBase struct {
	unexportedBodyBase

	ID   string `path:"id"`
	Name string `json:"name"`
}

// ResponseWithUnexportedBase mixes a header with an embedded unexported-named
// body struct, so extractUnifiedResponseComponents takes its tag-aware path.
type ResponseWithUnexportedBase struct {
	unexportedBodyBase

	ETag string `header:"ETag"`
	Name string `json:"name"`
}

// paramNames returns the sorted names of the given parameters.
func paramNames(params []Parameter) []string {
	names := make([]string, 0, len(params))
	for _, p := range params {
		names = append(names, p.Name)
	}

	slices.Sort(names)

	return names
}

func TestQueryParamsPromoteEmbeddedUnexportedNamedStruct(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	got := paramNames(generateQueryParamsFromStruct(gen, ListWithUnexportedQueryBase{}))

	want := []string{"page", "search"}
	if !slices.Equal(got, want) {
		t.Errorf("query params = %v, want %v", got, want)
	}
}

func TestQueryParamsPromoteNestedEmbeddedUnexportedNamedStruct(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	got := paramNames(generateQueryParamsFromStruct(gen, ListWithNestedUnexportedQueryBase{}))

	want := []string{"limit", "search", "sort"}
	if !slices.Equal(got, want) {
		t.Errorf("query params = %v, want %v", got, want)
	}
}

func TestHeaderParamsPromoteEmbeddedUnexportedNamedStruct(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	got := paramNames(generateHeaderParamsFromStruct(gen, ListWithUnexportedHeaderBase{}))

	want := []string{"X-Tenant"}
	if !slices.Equal(got, want) {
		t.Errorf("header params = %v, want %v", got, want)
	}
}

func TestHeaderParamsPromoteNestedEmbeddedUnexportedNamedStruct(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	got := paramNames(generateHeaderParamsFromStruct(gen, ListWithNestedUnexportedHeaderBase{}))

	want := []string{"X-Actor", "X-Region"}
	if !slices.Equal(got, want) {
		t.Errorf("header params = %v, want %v", got, want)
	}
}

func TestUnifiedRequestComponentsPromoteEmbeddedUnexportedNamedStruct(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	components, err := extractUnifiedRequestComponents(gen, RequestWithUnexportedBase{})
	if err != nil {
		t.Fatalf("extractUnifiedRequestComponents error: %v", err)
	}

	if components.BodySchema == nil {
		t.Fatal("no body schema generated")
	}

	got := slices.Sorted(maps.Keys(components.BodySchema.Properties))

	want := []string{"item_id", "name"}
	if !slices.Equal(got, want) {
		t.Errorf("body properties = %v, want %v", got, want)
	}
}

func TestUnifiedResponseComponentsPromoteEmbeddedUnexportedNamedStruct(t *testing.T) {
	gen := newSchemaGenerator(make(map[string]*Schema), nil)

	components, err := extractUnifiedResponseComponents(gen, ResponseWithUnexportedBase{})
	if err != nil {
		t.Fatalf("extractUnifiedResponseComponents error: %v", err)
	}

	if components.BodySchema == nil {
		t.Fatal("no body schema generated")
	}

	got := slices.Sorted(maps.Keys(components.BodySchema.Properties))

	want := []string{"item_id", "name"}
	if !slices.Equal(got, want) {
		t.Errorf("body properties = %v, want %v", got, want)
	}
}
