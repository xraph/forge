package router

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRouteInfo_OpenAPIFieldsSurviveEveryConstructor pins the defect that
// motivated newRouteInfo: RouteInfo was assembled by hand at every accessor,
// and only Routes() remembered to copy the OpenAPI fields across. The other
// accessors silently returned the zero value, so any consumer that reached a
// route through them saw a route that was never deprecated and had no
// operation ID.
func TestRouteInfo_OpenAPIFieldsSurviveEveryConstructor(t *testing.T) {
	r := NewRouter()

	err := r.GET("/widgets", func(ctx Context) error {
		return ctx.String(200, "ok")
	},
		WithName("list-widgets"),
		WithOperationID("listWidgets"),
		WithDeprecated(),
		WithTags("widgets"),
		WithMetadata("owner", "platform"),
	)
	require.NoError(t, err)

	byName, found := r.RouteByName("list-widgets")
	require.True(t, found)

	constructors := map[string]RouteInfo{
		"Routes":           onlyRoute(t, r.Routes()),
		"RouteByName":      byName,
		"RoutesByTag":      onlyRoute(t, r.RoutesByTag("widgets")),
		"RoutesByMetadata": onlyRoute(t, r.RoutesByMetadata("owner", "platform")),
	}

	for name, info := range constructors {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, "listWidgets", info.OperationID, "OperationID dropped by %s", name)
			assert.True(t, info.Deprecated, "Deprecated dropped by %s", name)
		})
	}
}

// TestRouteInfo_ConstructorsAgreeFieldForField is the guard against the
// divergence coming back. Every path that hands out a RouteInfo for the same
// route must hand out the same RouteInfo, field for field — including the one
// register() builds for interceptors, which is the only path with live
// consequences today.
//
// No field is excluded. If a future field legitimately has to differ by
// constructor, exclude it here explicitly and say why; a silent difference is
// the bug this test exists to catch.
func TestRouteInfo_ConstructorsAgreeFieldForField(t *testing.T) {
	r := NewRouter()

	var intercepted *RouteInfo

	capture := NewInterceptor("capture", func(ctx Context, route RouteInfo) InterceptorResult {
		snapshot := route
		intercepted = &snapshot

		return Allow()
	})

	middleware := func(next Handler) Handler {
		return func(ctx Context) error { return next(ctx) }
	}

	err := r.GET("/widgets/:id", func(ctx Context) error {
		return ctx.String(200, "ok")
	},
		WithName("get-widget"),
		WithOperationID("getWidget"),
		WithDeprecated(),
		WithSummary("Get a widget"),
		WithDescription("Returns a single widget by id."),
		WithTags("widgets", "public"),
		WithMetadata("owner", "platform"),
		WithExtension("probe", &stubExtension{name: "probe"}),
		WithMiddleware(middleware),
		WithInterceptor(capture),
		WithSkipInterceptor("some-other-interceptor"),
		WithSensitiveFieldCleaning(),
		WithTimeout(3*time.Second),
	)
	require.NoError(t, err)

	// Drive a request so the interceptor sees the RouteInfo register() built.
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/widgets/42", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, intercepted, "interceptor never ran; it cannot vouch for register()'s RouteInfo")

	byName, found := r.RouteByName("get-widget")
	require.True(t, found)

	cases := []struct {
		name string
		info RouteInfo
	}{
		{"Routes", onlyRoute(t, r.Routes())},
		{"RouteByName", byName},
		{"RoutesByTag", onlyRoute(t, r.RoutesByTag("widgets"))},
		{"RoutesByMetadata", onlyRoute(t, r.RoutesByMetadata("owner", "platform"))},
		{"register/interceptor", *intercepted},
	}

	want := routeInfoFields(cases[0].info)

	// Guard the guard: a RouteInfo whose fields all rendered empty would make
	// every comparison below pass vacuously.
	require.Equal(t, reflect.TypeOf(RouteInfo{}).NumField(), len(want))
	require.Equal(t, "getWidget", want["OperationID"])
	require.Equal(t, "true", want["Deprecated"])

	for _, tc := range cases[1:] {
		t.Run(tc.name, func(t *testing.T) {
			got := routeInfoFields(tc.info)
			for field, wantVal := range want {
				assert.Equal(t, wantVal, got[field],
					"%s disagrees with %s on RouteInfo.%s", tc.name, cases[0].name, field)
			}
		})
	}
}

func onlyRoute(t *testing.T, infos []RouteInfo) RouteInfo {
	t.Helper()
	require.Len(t, infos, 1)

	return infos[0]
}

// routeInfoFields renders every field of a RouteInfo to a comparable string.
// It reflects over the struct rather than listing fields, so a field added to
// RouteInfo is covered by the guard without anyone remembering to add it here.
func routeInfoFields(info RouteInfo) map[string]string {
	v := reflect.ValueOf(info)
	t := v.Type()

	fields := make(map[string]string, t.NumField())
	for i := range t.NumField() {
		fields[t.Field(i).Name] = fieldSignature(v.Field(i))
	}

	return fields
}

// fieldSignature renders a value so that two RouteInfo values built from the
// same route compare equal. Funcs and pointers reduce to their identity, which
// is what "the same route" means here: the constructors share one *route, so
// anything they copy across must be the identical function or object, not
// merely an equal-looking one.
func fieldSignature(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Invalid:
		return "<invalid>"

	case reflect.Func, reflect.Pointer, reflect.UnsafePointer:
		if v.IsNil() {
			return "nil"
		}

		return fmt.Sprintf("%s@%#x", v.Kind(), v.Pointer())

	case reflect.Interface:
		if v.IsNil() {
			return "nil"
		}

		return fieldSignature(v.Elem())

	case reflect.Slice:
		if v.IsNil() {
			return "nil"
		}

		parts := make([]string, v.Len())
		for i := range v.Len() {
			parts[i] = fieldSignature(v.Index(i))
		}

		return "[" + strings.Join(parts, " ") + "]"

	case reflect.Map:
		if v.IsNil() {
			return "nil"
		}

		parts := make([]string, 0, v.Len())
		for _, k := range v.MapKeys() {
			parts = append(parts, fmt.Sprintf("%v=%s", k.Interface(), fieldSignature(v.MapIndex(k))))
		}

		sort.Strings(parts)

		return "{" + strings.Join(parts, " ") + "}"

	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// stubExtension is the minimum that satisfies Extension so the guard route can
// populate RouteInfo.Extensions.
type stubExtension struct{ name string }

func (e *stubExtension) Name() string                 { return e.name }
func (e *stubExtension) Version() string              { return "0.0.1" }
func (e *stubExtension) Description() string          { return "test extension" }
func (e *stubExtension) Start(context.Context) error  { return nil }
func (e *stubExtension) Stop(context.Context) error   { return nil }
func (e *stubExtension) Health(context.Context) error { return nil }
func (e *stubExtension) Dependencies() []string       { return nil }
