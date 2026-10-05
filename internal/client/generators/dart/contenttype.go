package dart

import "strings"

// textApplicationTypes are the application/* types that carry text, so a
// client hands them over as a String rather than as bytes.
var textApplicationTypes = map[string]bool{
	"application/xml": true, "application/yaml": true, "application/x-yaml": true,
	"application/javascript": true, "application/x-javascript": true, "application/ecmascript": true,
	"application/x-www-form-urlencoded": true, "application/graphql": true, "application/sql": true,
	"application/x-ndjson": true, "application/jsonl": true,
}

// isTextMediaType reports whether a content type that is not JSON carries
// text: text/*, any +xml or +yaml type, or one of textApplicationTypes.
func isTextMediaType(contentType string) bool {
	essence := mediaEssence(contentType)

	return strings.HasPrefix(essence, "text/") || strings.HasSuffix(essence, "+xml") ||
		strings.HasSuffix(essence, "+yaml") || textApplicationTypes[essence]
}

// mediaKind classifies a content type: "json", "text" or "bytes". It is the one
// rule the planner applies to requests and responses, and the rule forge_client's
// transport and the generated REST client apply to what comes back (see
// _mediaKind in transport.dart and RestClient._text in rest.go), so the three
// agree on what is JSON, what is text and what is bytes.
func mediaKind(contentType string) string {
	switch {
	case isJSONMediaType(contentType):
		return "json"
	case isTextMediaType(contentType):
		return "text"
	}

	return "bytes"
}
