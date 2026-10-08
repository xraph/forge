package queue

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/xraph/forge/errors"
)

// redactURL returns raw with its password replaced by "xxxxx", fit for logs
// and errors. When raw does not parse there is no telling where the password
// ends, so everything between the scheme and the last '@' goes instead.
func redactURL(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Opaque == "" {
		return u.Redacted()
	}

	at := strings.LastIndex(raw, "@")
	if at < 0 {
		return raw
	}

	prefix := ""
	if scheme, _, ok := strings.Cut(raw, "://"); ok {
		prefix = scheme + "://"
	}

	return prefix + "xxxxx@" + raw[at+1:]
}

// redactURLList redacts each URL in a comma-separated server list, the form
// NATS accepts.
func redactURLList(raw string) string {
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		parts[i] = redactURL(strings.TrimSpace(p))
	}

	return strings.Join(parts, ",")
}

// redactURLError rewrites a *url.Error anywhere in err's chain, since its
// message quotes the whole URL, password included. Any other error is
// returned unchanged.
func redactURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}

	cause := ue.Err.Error()

	var esc url.EscapeError
	if errors.As(ue.Err, &esc) {
		// The bad escape is quoted verbatim and may be part of the password.
		cause = "invalid URL escape"
	}

	return fmt.Errorf("%s %q: %s", ue.Op, redactURL(ue.URL), cause)
}
