package router

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/internal/router/testtypes/billing"
	"github.com/xraph/forge/internal/router/testtypes/shipping"
	"github.com/xraph/forge/internal/router/testtypes/warehouse"
	"github.com/xraph/forge/internal/shared"
)

// pinnedGadgetResponse pins the component name "Gadget" onto billing.Invoice
// with a `schema:"..."` tag. The header field is what routes this body through
// registerPinnedComponent rather than the inferred-name path.
type pinnedGadgetResponse struct {
	ETag string          `header:"ETag"`
	Body billing.Invoice `body:""       json:"body" schema:"Gadget"`
}

// GadgetKind is an enum whose EnumNamer pins the component name "Gadget",
// contesting the inferred name of warehouse.Gadget.
type GadgetKind string

func (GadgetKind) EnumValues() []any {
	return []any{"widget", "sprocket"}
}

func (GadgetKind) EnumComponentName() string {
	return "Gadget"
}

func (k GadgetKind) MarshalText() ([]byte, error) {
	return []byte(k), nil
}

// gadgetKindHolder carries the enum so that generating its schema registers the
// pinned enum component.
type gadgetKindHolder struct {
	Kind GadgetKind `json:"kind"`
}

// registerPinnedGadget declares a route whose 200 response body is pinned to
// the component name "Gadget".
func registerPinnedGadget(t *testing.T, r Router) {
	t.Helper()
	require.NoError(t, r.GET("/pinned", func(ctx Context) error { return nil },
		WithResponseSchema(200, "pinned", pinnedGadgetResponse{})))
}

// registerInferredGadget declares a route returning warehouse.Gadget, whose
// inferred component name is "Gadget".
func registerInferredGadget(t *testing.T, r Router) {
	t.Helper()
	require.NoError(t, r.GET("/inferred",
		func(ctx shared.Context, req *collisionEmptyRequest) (*warehouse.Gadget, error) {
			return &warehouse.Gadget{}, nil
		}))
}

// registerEnumGadget declares a route whose response embeds the enum that pins
// the component name "Gadget".
func registerEnumGadget(t *testing.T, r Router) {
	t.Helper()
	require.NoError(t, r.GET("/enum",
		func(ctx shared.Context, req *collisionEmptyRequest) (*gadgetKindHolder, error) {
			return &gadgetKindHolder{}, nil
		}))
}

// TestOpenAPI_PinnedNameVersusInferredName is the hole the first collision pass
// left open: an explicitly pinned name contesting an inferred one. The pin wins
// the bare name -- the user asked for that exact string -- and the inferred type
// qualifies, in either registration order.
func TestOpenAPI_PinnedNameVersusInferredName(t *testing.T) {
	for _, tc := range []struct {
		name        string
		pinnedFirst bool
	}{
		{name: "PinnedFirst", pinnedFirst: true},
		{name: "InferredFirst", pinnedFirst: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "PinVsInferred", Version: "1.0.0"}))

			if tc.pinnedFirst {
				registerPinnedGadget(t, r)
				registerInferredGadget(t, r)
			} else {
				registerInferredGadget(t, r)
				registerPinnedGadget(t, r)
			}

			spec := r.OpenAPISpec()
			require.NotNil(t, spec)
			requireEveryRefResolves(t, spec)

			pinnedName, pinnedSchema := refTarget(t, spec, "/pinned")
			inferredName, inferredSchema := refTarget(t, spec, "/inferred")

			require.NotEqual(t, pinnedName, inferredName,
				"a pinned name and an inferred name collapsed onto one component: %s", pinnedName)

			// The explicit pin owns the bare name.
			require.Equal(t, "Gadget", pinnedName,
				"an explicitly pinned name must never be rewritten")
			requireProperty(t, pinnedSchema, "invoice_number")

			// The inferred type qualifies out of the way, keeping its own shape.
			requireProperty(t, inferredSchema, "serial_number")
			require.NotContains(t, inferredSchema.Properties, "invoice_number")
		})
	}
}

// TestOpenAPI_PinnedNameVersusInferredName_OrderIndependent pins the outcome
// itself, not just its shape: the names must not depend on which route was
// declared first.
func TestOpenAPI_PinnedNameVersusInferredName_OrderIndependent(t *testing.T) {
	build := func(pinnedFirst bool) *OpenAPISpec {
		r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "PinOrder", Version: "1.0.0"}))

		if pinnedFirst {
			registerPinnedGadget(t, r)
			registerInferredGadget(t, r)
		} else {
			registerInferredGadget(t, r)
			registerPinnedGadget(t, r)
		}

		return r.OpenAPISpec()
	}

	forward, backward := build(true), build(false)

	require.Equal(t, componentNames(forward), componentNames(backward),
		"component names must not depend on registration order")

	forwardName, _ := refTarget(t, forward, "/inferred")
	backwardName, _ := refTarget(t, backward, "/inferred")
	require.Equal(t, forwardName, backwardName)
}

// TestOpenAPI_PinnedEnumNameVersusInferredName is the same contest with the
// pin coming from an EnumNamer. The failure this guards against is worse than a
// dropped component: the object endpoint used to $ref the enum schema, so a
// caller reading the document was told an object was a string.
func TestOpenAPI_PinnedEnumNameVersusInferredName(t *testing.T) {
	for _, tc := range []struct {
		name      string
		enumFirst bool
	}{
		{name: "EnumFirst", enumFirst: true},
		{name: "InferredFirst", enumFirst: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "EnumVsInferred", Version: "1.0.0"}))

			if tc.enumFirst {
				registerEnumGadget(t, r)
				registerInferredGadget(t, r)
			} else {
				registerInferredGadget(t, r)
				registerEnumGadget(t, r)
			}

			spec := r.OpenAPISpec()
			require.NotNil(t, spec)
			requireEveryRefResolves(t, spec)

			inferredName, inferredSchema := refTarget(t, spec, "/inferred")

			// The object endpoint must describe an object, never the enum that
			// happened to want the same name.
			require.Empty(t, inferredSchema.Enum,
				"the object endpoint resolves to an enum schema under %q", inferredName)
			requireProperty(t, inferredSchema, "serial_number")

			// The enum keeps the name its EnumNamer pinned.
			enumSchema, ok := spec.Components.Schemas["Gadget"]
			require.True(t, ok, "the pinned enum name went missing: %v", componentNames(spec))
			require.Equal(t, "string", enumSchema.Type)
			require.NotEmpty(t, enumSchema.Enum)

			require.NotEqual(t, "Gadget", inferredName,
				"the object type kept the name the enum pinned")
		})
	}
}

// TestOpenAPI_PinnedEnumNameVersusInferredName_OrderIndependent pins the
// order-dependence that made the enum case resolve one way and break the other.
func TestOpenAPI_PinnedEnumNameVersusInferredName_OrderIndependent(t *testing.T) {
	build := func(enumFirst bool) *OpenAPISpec {
		r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "EnumOrder", Version: "1.0.0"}))

		if enumFirst {
			registerEnumGadget(t, r)
			registerInferredGadget(t, r)
		} else {
			registerInferredGadget(t, r)
			registerEnumGadget(t, r)
		}

		return r.OpenAPISpec()
	}

	forward, backward := build(true), build(false)

	require.Equal(t, componentNames(forward), componentNames(backward),
		"component names must not depend on registration order")

	forwardName, _ := refTarget(t, forward, "/inferred")
	backwardName, _ := refTarget(t, backward, "/inferred")
	require.Equal(t, forwardName, backwardName)
}

// TestOpenAPI_PinnedContest_StableAcrossGenerations pins the risk that comes
// with settling a pin mid-generation: eviction rewrites the type registries and
// the registration map, and those survive beginSpec. If the second pass started
// from a different state than the first, the document would change under a
// caller who only asked for it twice.
// Both pin mechanisms are covered: the tag pin and the EnumNamer pin reach
// pinComponentName with different scopes, so they touch the surviving state
// differently. They cannot appear in one router here -- two types pinned to
// "Gadget" is the unresolvable case, which TestOpenAPI_TwoExplicitPinsOnOneName
// covers -- so each gets its own run.
func TestOpenAPI_PinnedContest_StableAcrossGenerations(t *testing.T) {
	for _, tc := range []struct {
		name string
		pin  func(*testing.T, Router)
	}{
		{name: "SchemaTag", pin: registerPinnedGadget},
		{name: "EnumNamer", pin: registerEnumGadget},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "PinStable", Version: "1.0.0"}))

			registerInferredGadget(t, r)
			tc.pin(t, r)

			first := r.OpenAPISpec()
			require.NotNil(t, first)

			firstJSON, err := json.Marshal(first)
			require.NoError(t, err)

			for range 5 {
				next := r.OpenAPISpec()
				require.NotNil(t, next)
				requireEveryRefResolves(t, next)
				require.Equal(t, componentNames(first), componentNames(next))

				nextJSON, err := json.Marshal(next)
				require.NoError(t, err)
				require.JSONEq(t, string(firstJSON), string(nextJSON))
			}
		})
	}
}

// TestOpenAPI_PinArrivesAfterFirstGeneration is the eviction case at its most
// awkward: the inferred type already has a component and a $ref in a document
// that was generated and handed out, and only then does a pin claim its name.
func TestOpenAPI_PinArrivesAfterFirstGeneration(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "LatePin", Version: "1.0.0"}))

	registerInferredGadget(t, r)

	first := r.OpenAPISpec()
	require.NotNil(t, first)
	requireEveryRefResolves(t, first)

	// Uncontested so far, so the inferred type holds the bare name.
	firstName, _ := refTarget(t, first, "/inferred")
	require.Equal(t, "Gadget", firstName)

	registerPinnedGadget(t, r)

	second := r.OpenAPISpec()
	require.NotNil(t, second)
	requireEveryRefResolves(t, second)

	pinnedName, pinnedSchema := refTarget(t, second, "/pinned")
	inferredName, inferredSchema := refTarget(t, second, "/inferred")

	require.Equal(t, "Gadget", pinnedName)
	requireProperty(t, pinnedSchema, "invoice_number")

	require.NotEqual(t, "Gadget", inferredName,
		"the inferred type kept a name the pin had claimed")
	requireProperty(t, inferredSchema, "serial_number")
}

// TestOpenAPI_PinnedVersusInferredIsReported checks the message a user actually
// reads. The old wording claimed the name was "claimed explicitly by two types",
// which is false here: only one side was explicit.
func TestOpenAPI_PinnedVersusInferredIsReported(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "PinReport", Version: "1.0.0"}))

	registerPinnedGadget(t, r)
	registerInferredGadget(t, r)

	impl, ok := r.(*router)
	require.True(t, ok)

	gen, ok := impl.openAPIGenerator.(*openAPIGenerator)
	require.True(t, ok)

	require.NotNil(t, r.OpenAPISpec())

	var report string

	for _, line := range gen.schemas.nameCollisions {
		if strings.Contains(line, `"Gadget"`) {
			report = line
		}
	}

	require.NotEmpty(t, report, "the contest went unreported: %v", gen.schemas.nameCollisions)
	require.Contains(t, report, "testtypes/billing.Invoice")
	require.Contains(t, report, "testtypes/warehouse.Gadget")
	require.NotContains(t, report, "claimed explicitly by two types",
		"only one side of this contest was explicit")
}

// TestOpenAPI_TwoExplicitPinsOnOneName is the genuinely unresolvable case: two
// types both pinned to the same string. Nothing can be qualified without
// disobeying one of the two explicit instructions, so generation fails rather
// than shipping a document in which one endpoint describes the other's type.
func TestOpenAPI_TwoExplicitPinsOnOneName(t *testing.T) {
	type firstResp struct {
		ETag string          `header:"ETag"`
		Body billing.Invoice `body:""       json:"body" schema:"Shared"`
	}

	type secondResp struct {
		ETag string           `header:"ETag"`
		Body shipping.Invoice `body:""       json:"body" schema:"Shared"`
	}

	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "DoublePin", Version: "1.0.0"}))

	require.NoError(t, r.GET("/first", func(ctx Context) error { return nil },
		WithResponseSchema(200, "first", firstResp{})))
	require.NoError(t, r.GET("/second", func(ctx Context) error { return nil },
		WithResponseSchema(200, "second", secondResp{})))

	impl, ok := r.(*router)
	require.True(t, ok)

	gen, ok := impl.openAPIGenerator.(*openAPIGenerator)
	require.True(t, ok)

	spec, err := gen.Generate()
	require.Error(t, err, "a double pin must fail generation, not warn")
	require.Nil(t, spec)

	for _, want := range []string{
		`"Shared"`,
		"pinned to two different types",
		"testtypes/billing.Invoice",
		"testtypes/shipping.Invoice",
	} {
		require.Contains(t, err.Error(), want)
	}

	// It must fail on every call, not only the first: the warning is
	// deduplicated across regenerations, but the failure cannot be.
	_, again := gen.Generate()
	require.Error(t, again)
	require.Equal(t, err.Error(), again.Error())

	// And it is still reported through the collision log, once.
	var conflict string

	for _, line := range gen.schemas.nameCollisions {
		if strings.Contains(line, `"Shared"`) {
			require.Empty(t, conflict, "the double pin was reported more than once")

			conflict = line
		}
	}

	require.NotEmpty(t, conflict, "an unresolvable double pin must be reported: %v",
		gen.schemas.nameCollisions)
}

// TestOpenAPI_PinsFromDifferentMechanismsOnOneName is the same clash across the
// two pin mechanisms rather than within one: a `schema:"..."` tag and an
// EnumNamer that both answer "Gadget". Where the name was typed does not change
// that both were typed, so it fails the same way.
func TestOpenAPI_PinsFromDifferentMechanismsOnOneName(t *testing.T) {
	r := NewRouter(WithOpenAPI(OpenAPIConfig{Title: "MixedPins", Version: "1.0.0"}))

	registerPinnedGadget(t, r)
	registerEnumGadget(t, r)

	impl, ok := r.(*router)
	require.True(t, ok)

	gen, ok := impl.openAPIGenerator.(*openAPIGenerator)
	require.True(t, ok)

	spec, err := gen.Generate()
	require.Error(t, err)
	require.Nil(t, spec)
	require.Contains(t, err.Error(), "pinned to two different types")
	require.Contains(t, err.Error(), "testtypes/billing.Invoice")
	require.Contains(t, err.Error(), "router.GadgetKind")
}
