package golang

import "testing"

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
