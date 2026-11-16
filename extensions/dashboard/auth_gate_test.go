package dashboard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/forge"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

// headerChecker signs in whoever the X-Test-User header names, with the roles
// in X-Test-Roles. No header means nobody is signed in.
var headerChecker = dashauth.AuthCheckerFunc(func(_ context.Context, r *http.Request) (*dashauth.UserInfo, error) {
	sub := r.Header.Get("X-Test-User")
	if sub == "" {
		return nil, nil //nolint:nilnil // AuthChecker reports "nobody signed in" as nil, nil.
	}

	var roles []string
	if v := r.Header.Get("X-Test-Roles"); v != "" {
		roles = strings.Split(v, ",")
	}

	return &dashauth.UserInfo{Subject: sub, Roles: roles}, nil
})

func startGatedDashboard(t *testing.T, opts ...ConfigOption) (forge.App, *Extension) {
	t.Helper()

	off := false
	app := forge.New(forge.WithAppName("dash-gate"), forge.WithAppTracingConfig(forge.TracingConfig{Enabled: &off}))

	ext := NewExtension(opts...).(*Extension)
	ext.SetAuthChecker(headerChecker)

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	if err := ext.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ext.Stop(context.Background()) })

	return app, ext
}

type gateCall struct {
	name, method, path, body string
}

var gatedRoutes = []gateCall{
	{"overview", http.MethodPost, "/dashboard/api/dashboard/v1", `{"envelope":"v1","kind":"query","contributor":"core-contract","intent":"overview"}`},
	{"services.list", http.MethodPost, "/dashboard/api/dashboard/v1", `{"envelope":"v1","kind":"query","contributor":"core-contract","intent":"services.list"}`},
	{"traces.list", http.MethodPost, "/dashboard/api/dashboard/v1", `{"envelope":"v1","kind":"query","contributor":"core-contract","intent":"traces.list"}`},
	{"audit.list", http.MethodPost, "/dashboard/api/dashboard/v1", `{"envelope":"v1","kind":"query","contributor":"core-contract","intent":"audit.list"}`},
	{"extensions.list", http.MethodPost, "/dashboard/api/dashboard/v1", `{"envelope":"v1","kind":"query","contributor":"core-contract","intent":"extensions.list"}`},
	{"capabilities", http.MethodGet, "/dashboard/api/dashboard/v1/capabilities", ""},
	{"stream", http.MethodGet, "/dashboard/api/dashboard/v1/stream", ""},
	{"stream/control", http.MethodPost, "/dashboard/api/dashboard/v1/stream/control", `{}`},
	{"csrf", http.MethodGet, "/dashboard/api/dashboard/v1/csrf", ""},
	{"export/json", http.MethodGet, "/dashboard/export/json", ""},
	{"export/csv", http.MethodGet, "/dashboard/export/csv", ""},
	{"export/prometheus", http.MethodGet, "/dashboard/export/prometheus", ""},
}

func serveGate(t *testing.T, app forge.App, c gateCall, user, roles string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), c.method, c.path, strings.NewReader(c.body))
	if c.body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	if user != "" {
		req.Header.Set("X-Test-User", user)
		req.Header.Set("X-Test-Roles", roles)
	}

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	return rec
}

// Regression: with EnableAuth and RequiredRoles set, the data routes used to
// answer an anonymous caller with 200 and real data because only the
// principal endpoint looked at the user. Every route must now refuse.
func TestAuthGate_AnonymousRefusedOnEveryRoute(t *testing.T) {
	app, _ := startGatedDashboard(t, WithEnableAuth(true), WithRequiredRoles([]string{"admin"}))

	for _, c := range gatedRoutes {
		rec := serveGate(t, app, c, "", "")
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s: anonymous got %d, want 401; body=%s", c.name, rec.Code, rec.Body)

			continue
		}

		var resp struct {
			OK    bool `json:"ok"`
			Error struct {
				Code    string         `json:"code"`
				Details map[string]any `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Errorf("%s: body is not an error envelope: %v", c.name, err)

			continue
		}

		if resp.OK || resp.Error.Code != "UNAUTHENTICATED" || resp.Error.Details["loginPath"] != "/dashboard/login" {
			t.Errorf("%s: unexpected refusal body %s", c.name, rec.Body)
		}
	}

	// The principal endpoint keeps its own 401 shape, which carries the
	// login path at the top level for the client's redirect.
	rec := serveGate(t, app, gateCall{"principal", http.MethodGet, "/dashboard/api/dashboard/v1/principal", ""}, "", "")
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), `"loginPath":"/dashboard/login"`) {
		t.Errorf("principal: got %d %s", rec.Code, rec.Body)
	}
}

func TestAuthGate_MissingRoleForbidden(t *testing.T) {
	app, _ := startGatedDashboard(t, WithEnableAuth(true), WithRequiredRoles([]string{"admin"}))

	for _, c := range gatedRoutes {
		rec := serveGate(t, app, c, "bob", "viewer")
		if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "PERMISSION_DENIED") {
			t.Errorf("%s: user without the role got %d, want 403; body=%s", c.name, rec.Code, rec.Body)
		}
	}
}

func TestAuthGate_AllowedUserReachesData(t *testing.T) {
	app, _ := startGatedDashboard(t, WithEnableAuth(true), WithRequiredRoles([]string{"admin"}))

	for _, name := range []string{"overview", "extensions.list", "capabilities", "csrf", "export/json"} {
		var c gateCall

		for _, g := range gatedRoutes {
			if g.name == name {
				c = g
			}
		}

		rec := serveGate(t, app, c, "alice", "viewer,admin")
		if rec.Code != http.StatusOK {
			t.Errorf("%s: admin got %d, want 200; body=%s", name, rec.Code, rec.Body)
		}
	}

	rec := serveGate(t, app, gateCall{"principal", http.MethodGet, "/dashboard/api/dashboard/v1/principal", ""}, "alice", "admin")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"subject":"alice"`) {
		t.Errorf("principal: got %d %s", rec.Code, rec.Body)
	}
}

// With auth on but no role list, any signed-in user gets in.
func TestAuthGate_NoRequiredRolesAdmitsAnySignedInUser(t *testing.T) {
	app, _ := startGatedDashboard(t, WithEnableAuth(true))

	c := gatedRoutes[0]
	if rec := serveGate(t, app, c, "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", rec.Code)
	}

	if rec := serveGate(t, app, c, "carol", ""); rec.Code != http.StatusOK {
		t.Errorf("signed-in user got %d, want 200; body=%s", rec.Code, rec.Body)
	}
}

// A failing auth backend must close the door, not open it.
func TestAuthGate_CheckerErrorIsUnauthenticated(t *testing.T) {
	off := false
	app := forge.New(forge.WithAppName("dash-gate-err"), forge.WithAppTracingConfig(forge.TracingConfig{Enabled: &off}))

	ext := NewExtension(WithEnableAuth(true)).(*Extension)
	ext.SetAuthChecker(dashauth.AuthCheckerFunc(func(context.Context, *http.Request) (*dashauth.UserInfo, error) {
		return nil, context.DeadlineExceeded
	}))

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	if err := ext.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ext.Stop(context.Background()) })

	if rec := serveGate(t, app, gatedRoutes[0], "", ""); rec.Code != http.StatusUnauthorized {
		t.Errorf("got %d, want 401", rec.Code)
	}
}

// Auth off keeps the documented open behaviour.
func TestAuthGate_AuthOffLeavesRoutesOpen(t *testing.T) {
	app, _ := startGatedDashboard(t)

	for _, name := range []string{"overview", "export/json"} {
		for _, c := range gatedRoutes {
			if c.name != name {
				continue
			}

			if rec := serveGate(t, app, c, "", ""); rec.Code != http.StatusOK {
				t.Errorf("%s: got %d with auth off, want 200", name, rec.Code)
			}
		}
	}
}
