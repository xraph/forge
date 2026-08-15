package golang

import (
	"strings"
	"testing"

	"github.com/xraph/forge/internal/client"
)

func TestGoFieldName(t *testing.T) {
	cases := map[string]string{
		// camelCase is how OpenAPI documents conventionally key schemes, and
		// the generator's existing toPascalCase lowercases the tail, turning
		// bearerAuth into Bearerauth.
		"bearerAuth":    "BearerAuth",
		"sessionAuth":   "SessionAuth",
		"openIdConnect": "OpenIdConnect",
		"X-Tenant-Key":  "XTenantKey",
		"api_key":       "ApiKey",
		"api key":       "ApiKey",
		"oauth2/token":  "Oauth2Token",
		// Must be a valid exported identifier whatever arrives.
		"2fa":     "X2fa",
		"a.b$c":   "ABc",
		"_hidden": "Hidden",
		// Regression: non-ASCII scheme keys must not produce invalid UTF-8.
		"überAuth": "ÜberAuth",
		// All digits must be prefixed to form a valid identifier.
		"123": "X123",
		// Nothing usable left.
		"...": "",
		"":    "",
	}

	for in, want := range cases {
		if got := goFieldName(in); got != want {
			t.Errorf("goFieldName(%q) = %q, want %q", in, got, want)
		}
	}
}

func schemes() []client.DetectedAuthScheme {
	return []client.DetectedAuthScheme{
		{Key: "bearerAuth", Type: "http", Scheme: "bearer"},
		{Key: "basicAuth", Type: "http", Scheme: "basic"},
		// Deliberately not named X-API-Key: "emits X-API-Key regardless of the
		// declaration" is the defect under test.
		{Key: "tenantKey", Type: "apiKey", In: "header", ParamName: "X-Tenant-Key"},
		{Key: "sessionAuth", Type: "apiKey", In: "cookie", ParamName: "session_id"},
		{Key: "listKey", Type: "apiKey", In: "query", ParamName: "api_key"},
		{Key: "oidc", Type: "openIdConnect"},
	}
}

func TestGenerateAuthConfigEmitsAFieldPerScheme(t *testing.T) {
	code, warnings := generateAuthConfig(schemes())

	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	for _, want := range []string{
		"BearerAuth string",
		"BasicAuth BasicCredentials",
		"TenantKey string",
		"SessionAuth string",
		"ListKey string",
		"Oidc string",
		"CustomHeaders map[string]string",
		"type BasicCredentials struct",
	} {
		if !strings.Contains(code, want) {
			t.Errorf("AuthConfig missing %q\n%s", want, code)
		}
	}
}

func TestGenerateAuthConfigReportsCollidingKeys(t *testing.T) {
	// "api_key" and "api-key" both derive to the exact same field name,
	// "ApiKey" (goFieldName only cases the first rune of each part and
	// leaves the rest as written, so this is a genuine exact-string
	// collision -- unlike "api_key" vs "API-KEY", which derive to distinct,
	// legal identifiers "ApiKey" and "APIKEY" that compile fine side by
	// side).
	_, warnings := generateAuthConfig([]client.DetectedAuthScheme{
		{Key: "api_key", Type: "apiKey", In: "header", ParamName: "A"},
		{Key: "api-key", Type: "apiKey", In: "header", ParamName: "B"},
	})

	// Emitting both would produce a struct with a duplicate field, so the
	// generated package would not compile.
	if len(warnings) == 0 {
		t.Fatal("colliding keys produced no warning")
	}

	if !strings.Contains(warnings[0], "ApiKey") {
		t.Errorf("warning does not name the collision: %q", warnings[0])
	}
}

func TestGenerateAuthApplyUsesEachSchemesOwnLocation(t *testing.T) {
	code := generateAuthApply(schemes())

	for _, want := range []string{
		`header.Set("Authorization", "Bearer "+a.BearerAuth)`,
		`header.Set("Authorization", "Bearer "+a.Oidc)`,
		`header.Set("X-Tenant-Key", a.TenantKey)`,
		`header.Add("Cookie", "session_id="`,
		`q.Set("api_key", a.ListKey)`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("apply missing %q\n%s", want, code)
		}
	}

	// The bug that started this.
	if strings.Contains(code, "X-API-Key") {
		t.Errorf("apply still hardcodes X-API-Key\n%s", code)
	}
}
