package telemetry

import (
	"net/url"
	"strings"
)

// redacted stands in for a query value that isn't recorded, the value the
// OpenTelemetry semantic conventions use.
const redacted = "REDACTED"

// scrubQuery returns raw, a URL's encoded query string, with every value
// replaced by redacted unless its key is in allowed. Keys, their order and
// empty values stay as sent. A bare key (no "=") reads redacted unless it is
// allowed, because ?<token> puts the secret where the key goes. A key is
// matched after decoding, so allowed holds plain names. A nil allowed redacts
// every value and every bare key.
func scrubQuery(raw string, allowed map[string]struct{}) string {
	if raw == "" {
		return ""
	}

	var b strings.Builder

	b.Grow(len(raw))

	first := true

	for part := range strings.SplitSeq(raw, "&") {
		if !first {
			b.WriteByte('&')
		}

		first = false

		key, value, hasValue := strings.Cut(part, "=")
		if !hasValue {
			if key == "" || queryKeyAllowed(key, allowed) {
				b.WriteString(key)
			} else {
				b.WriteString(redacted)
			}

			continue
		}

		b.WriteString(key)
		b.WriteByte('=')

		if value == "" || queryKeyAllowed(key, allowed) {
			b.WriteString(value)
		} else {
			b.WriteString(redacted)
		}
	}

	return b.String()
}

func queryKeyAllowed(key string, allowed map[string]struct{}) bool {
	if len(allowed) == 0 {
		return false
	}

	if _, ok := allowed[key]; ok {
		return true
	}

	decoded, err := url.QueryUnescape(key)
	if err != nil || decoded == key {
		return false
	}

	_, ok := allowed[decoded]

	return ok
}

// keySet builds the lookup scrubQuery takes. It returns nil for no keys.
func keySet(keys []string) map[string]struct{} {
	if len(keys) == 0 {
		return nil
	}

	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}

	return set
}
