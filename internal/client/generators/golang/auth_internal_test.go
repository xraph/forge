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
		// Removing the ":" separator, or swapping Username/Password, fails no
		// other test -- this is the assertion that would actually catch it.
		`header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(a.BasicAuth.Username+":"+a.BasicAuth.Password)))`,
		`header.Set("Authorization", "Bearer "+a.Oidc)`,
		`header.Set("X-Tenant-Key", a.TenantKey)`,
		`header.Set("Cookie", "session_id="+a.SessionAuth)`,
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

// TestGenerateAuthApplyMergesMultipleCookieSchemes is the regression test for
// the defect a single cookie scheme cannot expose: net/http.Client.send calls
// Request.AddCookie once per jar cookie, and AddCookie is implemented as
// Header.Get("Cookie") (first value only) followed by Header.Set (replaces
// the whole slice). header.Add would put a second cookie scheme's value under
// a second slice entry that AddCookie's Get never sees and its Set then
// discards outright. One cookie scheme's emitted code looks identical whether
// apply uses Add or Set, so this fixture needs two.
func TestGenerateAuthApplyMergesMultipleCookieSchemes(t *testing.T) {
	code := generateAuthApply([]client.DetectedAuthScheme{
		{Key: "sessionAuth", Type: "apiKey", In: "cookie", ParamName: "sid"},
		{Key: "csrfAuth", Type: "apiKey", In: "cookie", ParamName: "csrf"},
	})

	for _, want := range []string{
		`header.Set("Cookie", "sid="+a.SessionAuth)`,
		`header.Set("Cookie", prev+"; sid="+a.SessionAuth)`,
		`header.Set("Cookie", "csrf="+a.CsrfAuth)`,
		`header.Set("Cookie", prev+"; csrf="+a.CsrfAuth)`,
	} {
		if !strings.Contains(code, want) {
			t.Errorf("apply missing %q\n%s", want, code)
		}
	}

	// Add would silently lose every scheme but the first the moment a jar is
	// installed (see AuthConfig.apply's cookie case for the full chain).
	if strings.Contains(code, `header.Add("Cookie"`) {
		t.Errorf("apply still uses Add for cookies\n%s", code)
	}
}

// TestGenerateAuthConfigWarnsOnUnhandledScheme covers the belief the earlier
// review recorded and this one overturned: that an unhandled type/scheme pair
// was unreachable. spec_parser.go and introspector.go copy Type and Scheme
// verbatim out of the document with no validation, so {type: http, scheme:
// digest} and {type: mutualTLS} (both legal) reach generateAuthConfig's
// switches. Before this fix, resolveAuthFields had already reserved the
// field name and the switch's missing default then consumed the scheme and
// emitted nothing -- a declared credential vanishing with no warning at all.
func TestGenerateAuthConfigWarnsOnUnhandledScheme(t *testing.T) {
	code, warnings := generateAuthConfig([]client.DetectedAuthScheme{
		{Key: "digestAuth", Type: "http", Scheme: "digest"},
		{Key: "mtls", Type: "mutualTLS"},
	})

	if len(warnings) != 2 {
		t.Fatalf("warnings = %v, want 2", warnings)
	}

	for _, want := range []string{"digestAuth", "http", "digest"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning %q does not name %q", warnings[0], want)
		}
	}

	for _, want := range []string{"mtls", "mutualTLS"} {
		if !strings.Contains(warnings[1], want) {
			t.Errorf("warning %q does not name %q", warnings[1], want)
		}
	}

	// Neither scheme has a defined wire encoding here, so neither should
	// have claimed a field in the struct.
	if strings.Contains(code, "DigestAuth") || strings.Contains(code, "Mtls") {
		t.Errorf("AuthConfig declares a field for an unhandled scheme\n%s", code)
	}
}

// TestGenerateAuthConfigWarnsOnUnhandledAPIKeyLocation covers the apiKey
// locations apply has no encoding for.
//
// OpenAPI 3.1 allows exactly "query", "header" and "cookie", but the two IR
// builders copy `in` verbatim out of the document with no validation, so a
// typo ("Header"), an OpenAPI 2.0 leftover ("body") or an absent value all
// reach generateAuthConfig. Every apiKey used to get a field whatever its
// location, while generateAuthApply's switch matches only the three it can
// encode. The result was a field you could set and that never reached the
// wire: worse than the vanished credential the surrounding warnings exist to
// prevent, because it looks like it worked.
func TestGenerateAuthConfigWarnsOnUnhandledAPIKeyLocation(t *testing.T) {
	unhandled := []client.DetectedAuthScheme{
		{Key: "bodyKey", Type: "apiKey", In: "body", ParamName: "token"},
		{Key: "casedKey", Type: "apiKey", In: "Header", ParamName: "X-Token"},
		{Key: "noLocation", Type: "apiKey", ParamName: "token"},
	}

	code, warnings := generateAuthConfig(unhandled)

	if len(warnings) != 3 {
		t.Fatalf("warnings = %v, want 3", warnings)
	}

	for i, want := range [][]string{
		{"bodyKey", "body"},
		{"casedKey", "Header"},
		{"noLocation", `""`},
	} {
		for _, fragment := range want {
			if !strings.Contains(warnings[i], fragment) {
				t.Errorf("warning %q does not name %q", warnings[i], fragment)
			}
		}
	}

	// A field for any of these is a credential that sets cleanly and goes
	// nowhere, so none should have claimed one.
	for _, name := range []string{"BodyKey", "CasedKey", "NoLocation"} {
		if strings.Contains(code, name) {
			t.Errorf("AuthConfig declares %q for an apiKey location apply cannot encode\n%s", name, code)
		}
	}

	// apply's default arm is documented as reachable only for schemes the
	// config step already warned about and skipped. Assert that rather than
	// trusting it: a field named here with no declaration above would not
	// compile.
	if apply := generateAuthApply(unhandled); strings.Contains(apply, "a.BodyKey") ||
		strings.Contains(apply, "a.CasedKey") || strings.Contains(apply, "a.NoLocation") {
		t.Errorf("apply reads a field AuthConfig never declared\n%s", apply)
	}
}

// TestGenerateAuthConfigKeepsEveryLegalAPIKeyLocation guards the other side of
// the switch above. The three OpenAPI 3.1 locations must keep their fields,
// and warn about nothing.
func TestGenerateAuthConfigKeepsEveryLegalAPIKeyLocation(t *testing.T) {
	code, warnings := generateAuthConfig([]client.DetectedAuthScheme{
		{Key: "headerKey", Type: "apiKey", In: "header", ParamName: "X-Tenant-Key"},
		{Key: "cookieKey", Type: "apiKey", In: "cookie", ParamName: "session_id"},
		{Key: "queryKey", Type: "apiKey", In: "query", ParamName: "api_key"},
	})

	if len(warnings) != 0 {
		t.Fatalf("warnings = %v, want none", warnings)
	}

	for _, want := range []string{"HeaderKey string", "CookieKey string", "QueryKey string"} {
		if !strings.Contains(code, want) {
			t.Errorf("AuthConfig missing %q\n%s", want, code)
		}
	}
}
