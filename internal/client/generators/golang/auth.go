package golang

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xraph/forge/internal/client"
)

// goFieldName turns a securityScheme key into an exported Go field name.
//
// Deliberately not `toPascalCase`, which lowercases everything after the first
// rune of each part and so turns `bearerAuth` into `Bearerauth`. camelCase is
// how OpenAPI documents conventionally key schemes, so reusing it would mangle
// nearly every real specification. `toPascalCase` is left alone because it has
// other callers whose generated identifiers would change, and that is not this
// change's business.
//
// Returns "" when nothing usable survives, which the caller reports as a spec
// problem rather than emitting a nameless field.
func goFieldName(key string) string {
	parts := strings.FieldsFunc(key, func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '/' || r == '.'
	})

	var out strings.Builder

	for _, part := range parts {
		// Keep the interior as written: `bearerAuth` -> `BearerAuth`.
		runes := []rune(part)
		out.WriteRune(unicode.ToUpper(runes[0]))

		for _, r := range runes[1:] {
			out.WriteRune(r)
		}
	}

	cleaned := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return r
		}

		return -1
	}, out.String())

	if cleaned == "" {
		return ""
	}

	r, size := utf8.DecodeRuneInString(cleaned)

	// A leading digit is not a Go identifier. Prefixing beats rejecting: `2fa`
	// is a plausible scheme key and `X2fa` is a usable field.
	if unicode.IsDigit(r) {
		return "X" + cleaned
	}

	// Rebuilt from the decoded rune, not re-sliced by byte: `cleaned` may lead
	// with a multi-byte rune, and byte-slicing one produces invalid UTF-8 and
	// therefore invalid Go source.
	return string(unicode.ToUpper(r)) + cleaned[size:]
}

// authField is one scheme resolved to the field the generated struct carries.
type authField struct {
	name   string // Go field name
	scheme client.DetectedAuthScheme
}

// resolveAuthFields maps schemes onto field names, reporting anything that
// cannot be emitted rather than emitting Go that does not compile.
//
// Collisions are compared on the exact field name, not case-folded. Two
// scheme keys that derive to field names differing only by case (e.g.
// "api_key" -> "ApiKey" vs "API-KEY" -> "APIKEY") are distinct, legal Go
// identifiers that compile fine side by side. Warning on that and skipping
// one would drop a credential the document declared -- exactly the failure
// this function exists to prevent -- in exchange for avoiding a merely
// confusing (not broken) pair of names. Only an exact match produces an
// actual duplicate field, which is the only case that does not compile.
func resolveAuthFields(schemes []client.DetectedAuthScheme) ([]authField, []string) {
	var (
		fields   []authField
		warnings []string
		taken    = map[string]string{} // field name -> the key that claimed it
	)

	for _, scheme := range schemes {
		name := goFieldName(scheme.Key)

		if name == "" {
			warnings = append(warnings, fmt.Sprintf(
				"security scheme %q has no usable Go field name and was skipped", scheme.Key))

			continue
		}

		if owner, clash := taken[name]; clash {
			// Two keys, one field: the struct would carry a duplicate and the
			// generated package would not build.
			warnings = append(warnings, fmt.Sprintf(
				"security schemes %q and %q both map to the field %q; %q was skipped",
				owner, scheme.Key, name, scheme.Key))

			continue
		}

		taken[name] = scheme.Key
		fields = append(fields, authField{name: name, scheme: scheme})
	}

	return fields, warnings
}

// generateAuthConfig emits the AuthConfig struct: one field per declared
// scheme, carrying its location and wire name in the comment.
func generateAuthConfig(schemes []client.DetectedAuthScheme) (string, []string) {
	fields, warnings := resolveAuthFields(schemes)

	var buf strings.Builder

	buf.WriteString("// BasicCredentials is one http basic scheme's username and password.\n")
	buf.WriteString("//\n")
	buf.WriteString("// A named struct rather than two flattened fields, so that two basic\n")
	buf.WriteString("// schemes do not turn into FooUsername/FooPassword sprawl.\n")
	buf.WriteString("type BasicCredentials struct{ Username, Password string }\n\n")

	buf.WriteString("// AuthConfig holds one value per security scheme the API declares.\n")
	buf.WriteString("type AuthConfig struct {\n")

	for _, f := range fields {
		switch f.scheme.Type {
		case "http":
			switch f.scheme.Scheme {
			case "bearer":
				buf.WriteString(fmt.Sprintf("\t%s string // http bearer -> Authorization: Bearer <v>\n", f.name))
			case "basic":
				buf.WriteString(fmt.Sprintf("\t%s BasicCredentials // http basic -> Authorization: Basic <v>\n", f.name))
			}
		case "apiKey":
			buf.WriteString(fmt.Sprintf("\t%s string // apiKey %s -> %s\n", f.name, f.scheme.In, f.scheme.ParamName))
		case "oauth2", "openIdConnect":
			// Neither specification mandates a transmission. Bearer is what the
			// flows produce in practice, and stating the assumption beats
			// implying the document made it.
			buf.WriteString(fmt.Sprintf("\t%s string // %s -> Authorization: Bearer <v>\n", f.name, f.scheme.Type))
		}
	}

	buf.WriteString("\tCustomHeaders map[string]string\n")
	buf.WriteString("}\n\n")

	return buf.String(), warnings
}

// generateAuthApply emits the single function every transport routes through.
//
// It takes a header and a URL rather than a *http.Request because a WebSocket
// handshake has both and no request. One implementation also means the REST and
// WebSocket paths cannot drift apart again, which is how the latter ended up
// bearer-only.
func generateAuthApply(schemes []client.DetectedAuthScheme) string {
	fields, _ := resolveAuthFields(schemes)

	var buf strings.Builder

	buf.WriteString("// apply writes every configured credential onto a request.\n")
	buf.WriteString("func (a *AuthConfig) apply(header http.Header, u *url.URL) {\n")
	buf.WriteString("\tif a == nil {\n\t\treturn\n\t}\n\n")

	for _, f := range fields {
		switch {
		case f.scheme.Type == "http" && f.scheme.Scheme == "bearer",
			f.scheme.Type == "oauth2", f.scheme.Type == "openIdConnect":
			buf.WriteString(fmt.Sprintf("\tif a.%s != \"\" {\n", f.name))
			buf.WriteString(fmt.Sprintf("\t\theader.Set(\"Authorization\", \"Bearer \"+a.%s)\n", f.name))
			buf.WriteString("\t}\n\n")

		case f.scheme.Type == "http" && f.scheme.Scheme == "basic":
			buf.WriteString(fmt.Sprintf("\tif a.%s.Username != \"\" || a.%s.Password != \"\" {\n", f.name, f.name))
			buf.WriteString(fmt.Sprintf("\t\theader.Set(\"Authorization\", \"Basic \"+base64.StdEncoding.EncodeToString([]byte(a.%s.Username+\":\"+a.%s.Password)))\n", f.name, f.name))
			buf.WriteString("\t}\n\n")

		case f.scheme.Type == "apiKey" && f.scheme.In == "header":
			buf.WriteString(fmt.Sprintf("\tif a.%s != \"\" {\n", f.name))
			buf.WriteString(fmt.Sprintf("\t\theader.Set(%q, a.%s)\n", f.scheme.ParamName, f.name))
			buf.WriteString("\t}\n\n")

		case f.scheme.Type == "apiKey" && f.scheme.In == "cookie":
			buf.WriteString(fmt.Sprintf("\tif a.%s != \"\" {\n", f.name))
			buf.WriteString(fmt.Sprintf("\t\theader.Add(\"Cookie\", %q+a.%s)\n", f.scheme.ParamName+"=", f.name))
			buf.WriteString("\t}\n\n")

		case f.scheme.Type == "apiKey" && f.scheme.In == "query":
			buf.WriteString(fmt.Sprintf("\tif a.%s != \"\" && u != nil {\n", f.name))
			buf.WriteString("\t\tq := u.Query()\n")
			buf.WriteString(fmt.Sprintf("\t\tq.Set(%q, a.%s)\n", f.scheme.ParamName, f.name))
			buf.WriteString("\t\tu.RawQuery = q.Encode()\n")
			buf.WriteString("\t}\n\n")
		}
	}

	// Last, so an application can still override anything above it. That was
	// the behaviour before this change and nothing here justifies altering it.
	buf.WriteString("\tfor key, value := range a.CustomHeaders {\n")
	buf.WriteString("\t\theader.Set(key, value)\n")
	buf.WriteString("\t}\n")
	buf.WriteString("}\n\n")

	return buf.String()
}
