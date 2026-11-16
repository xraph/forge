package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type cleanedEvent struct {
	Created time.Time  `json:"created"`
	Expires *time.Time `json:"expires"`
	Secret  string     `json:"secret"  sensitive:"true"`
	Note    string     `json:"note"`
}

type emptyReq struct{}

func serveTyped[T any](t *testing.T, handler func(Context, *emptyReq) (*T, error), opts ...RouteOption) *httptest.ResponseRecorder {
	t.Helper()

	r := NewRouter()
	require.NoError(t, r.POST("/x", handler, opts...))

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/x", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	return rec
}

// Cleaning built each struct copy from a zero value and filled in only the
// exported fields, so a time.Time, whose fields are all unexported, came out
// as 0001-01-01T00:00:00Z.
func TestSensitiveCleaning_KeepsTimeValues(t *testing.T) {
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	expires := created.Add(time.Hour)

	rec := serveTyped(t, func(ctx Context, _ *emptyReq) (*cleanedEvent, error) {
		return &cleanedEvent{Created: created, Expires: &expires, Secret: "s", Note: "n"}, nil
	}, WithSensitiveFieldCleaning())

	require.Equal(t, http.StatusOK, rec.Code)

	var got cleanedEvent
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))

	assert.True(t, got.Created.Equal(created), "created = %v", got.Created)
	require.NotNil(t, got.Expires)
	assert.True(t, got.Expires.Equal(expires), "expires = %v", got.Expires)
	assert.Empty(t, got.Secret)
	assert.Equal(t, "n", got.Note)
}

type withUnexported struct {
	Name     string `json:"name"`
	Password string `json:"password" sensitive:"true"`
	cache    string
}

func (w withUnexported) MarshalJSON() ([]byte, error) {
	type plain struct {
		Name     string `json:"name"`
		Password string `json:"password"`
		Cache    string `json:"cache"`
	}

	return json.Marshal(plain{Name: w.Name, Password: w.Password, Cache: w.cache})
}

// A type with its own MarshalJSON sees its unexported state, and its
// sensitive fields are still cleaned.
func TestSensitiveCleaning_KeepsUnexportedStateAndCleansMarshalers(t *testing.T) {
	in := withUnexported{Name: "a", Password: "p", cache: "c"}

	out, ok := cleanSensitiveFields(in).(withUnexported)
	require.True(t, ok)
	assert.Equal(t, withUnexported{Name: "a", cache: "c"}, out)
	assert.Equal(t, "p", in.Password, "the original must not be changed")
}

type namedSecret string

type namedHolder struct {
	Key  namedSecret  `json:"key"  sensitive:"redact"`
	Mask *namedSecret `json:"mask" sensitive:"mask:***"`
	Any  any          `json:"any"`
	Next *namedHolder `json:"next"`
}

func TestSensitiveCleaning_NamedStringsInterfacesAndRecursion(t *testing.T) {
	m := namedSecret("m")
	in := &namedHolder{
		Key:  "k",
		Mask: &m,
		Any:  UserResponse{ID: "1", Password: "p"},
		Next: &namedHolder{Key: "k2", Any: map[string]any{"u": &UserResponse{Password: "p2"}}},
	}

	out, ok := cleanSensitiveFields(in).(*namedHolder)
	require.True(t, ok)

	assert.Equal(t, namedSecret("[REDACTED]"), out.Key)
	require.NotNil(t, out.Mask)
	assert.Equal(t, namedSecret("***"), *out.Mask)
	assert.Equal(t, UserResponse{ID: "1", APIKey: "[REDACTED]", Token: "***"}, out.Any)
	assert.Equal(t, namedSecret("[REDACTED]"), out.Next.Key)

	nested, ok := out.Next.Any.(map[string]any)["u"].(*UserResponse)
	require.True(t, ok)
	assert.Empty(t, nested.Password)

	// The input is untouched.
	assert.Equal(t, namedSecret("k"), in.Key)
	assert.Equal(t, namedSecret("m"), m)
	assert.Equal(t, "p2", in.Next.Any.(map[string]any)["u"].(*UserResponse).Password)
}

type plainList struct {
	Items []struct {
		ID int `json:"id"`
	} `json:"items"`
}

// A value that holds nothing to clean is not copied.
func TestSensitiveCleaning_PassesThroughTypesWithNothingToClean(t *testing.T) {
	in := &plainList{}
	assert.Same(t, in, cleanSensitiveFields(in))

	now := time.Now()
	assert.Equal(t, now, cleanSensitiveFields(now))
}

type innerTagged struct {
	Trace string `header:"X-Inner"`
	Data  string `body:""`
}

type outerTagged struct {
	Request string      `header:"X-Request-ID"`
	Payload innerTagged `body:""`
}

// The response used to be processed twice: once by the router and again
// inside ctx.JSON. A body:"" value with header or body tags of its own was
// unwrapped a second time.
func TestTypedResponse_ProcessedOnce(t *testing.T) {
	rec := serveTyped(t, func(ctx Context, _ *emptyReq) (*outerTagged, error) {
		return &outerTagged{Request: "r1", Payload: innerTagged{Trace: "t1", Data: "d1"}}, nil
	})

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "r1", rec.Header().Get("X-Request-ID"))
	assert.Empty(t, rec.Header().Get("X-Inner"), "the unwrapped body must not be processed again")
	assert.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	assert.JSONEq(t, `{"Trace":"t1","Data":"d1"}`, rec.Body.String())
}

type unencodable struct {
	C chan int `json:"c"`
}

// The body is encoded before anything is written, so a value that cannot be
// encoded is a 500 rather than a 200 with a broken body.
func TestTypedResponse_EncodeFailureIs500(t *testing.T) {
	rec := serveTyped(t, func(ctx Context, _ *emptyReq) (*unencodable, error) {
		return &unencodable{C: make(chan int)}, nil
	})

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
