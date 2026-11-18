package plugins

import (
	"net/url"
	"regexp"
	"strings"
)

// redactedSecret replaces a password in redacted output. It matches what
// url.URL.Redacted writes, so every DSN shape reads the same way.
const redactedSecret = "xxxxx"

// sensitiveDSNParam matches a credential-bearing key=value pair in a libpq
// key/value DSN ("host=db password='a b' user=app"), a semicolon-separated
// connection string ("Server=db;Password=x;") or a URL query string
// ("?authToken=x&ssl=true"). A quoted value may contain spaces and escaped
// quotes; an unterminated quote runs to the end of the string so a malformed
// DSN still hides everything after the key instead of leaking the tail.
var sensitiveDSNParam = regexp.MustCompile(
	`(?i)(^|[\s?&;])(password|passwd|pass|pwd|sslpassword|_auth_pass|secret|token|authtoken|auth_token|access_token|apikey|api_key)(\s*=\s*)` +
		`('(?:[^'\\]|\\.)*(?:'|$)|"[^"]*(?:"|$)|[^\s;&]+)`,
)

// keyValueDSN reports whether a DSN starts like a key/value connection
// string ("host=db ..." or "Server=db;...") rather than user:pass@host.
var keyValueDSN = regexp.MustCompile(`^\s*[A-Za-z_][A-Za-z0-9_ ]*=`)

// redactDSN returns dsn with any password or token hidden, for printing in
// messages and errors. A DSN typed on the command line or read from config is
// echoed back on failure, and a terminal or CI log is the wrong place for the
// database password to end up.
//
// It understands URL DSNs (postgres://user:pass@host/db), go-sql-driver MySQL
// DSNs (user:pass@tcp(host)/db) and libpq key/value DSNs
// (host=x password=secret). When a URL DSN cannot be parsed and has no
// recognisable userinfo, only its scheme is kept, because the raw string may
// still hold a credential in a shape this function does not know.
func redactDSN(dsn string) string {
	if dsn == "" {
		return ""
	}

	if scheme, rest, ok := strings.Cut(dsn, "://"); ok {
		return redactSensitiveParams(redactURLDSN(dsn, scheme, rest))
	}

	// A key/value DSN has no userinfo, and cutting it at an '@' (an email in
	// user=, say) would mangle it, so only its key/value pairs are touched.
	if keyValueDSN.MatchString(dsn) {
		return redactSensitiveParams(dsn)
	}

	return redactSensitiveParams(redactUserinfo(dsn))
}

// redactURLDSN hides the password in a scheme://user:pass@host DSN. url.Parse
// is trusted only when the result is unambiguous: a single '@' that it
// actually read as userinfo and no '#'. An unescaped '@', '/', '?' or '#'
// inside a password makes url.Parse split the string somewhere else and
// Redacted would then print part of the password as host or path, so those
// cases fall back to cutting at the last '@'.
func redactURLDSN(dsn, scheme, rest string) string {
	u, err := url.Parse(dsn)
	if err == nil && !strings.Contains(dsn, "#") {
		switch at := strings.Count(rest, "@"); {
		case at == 0:
			return dsn
		case at == 1 && u.User != nil:
			return u.Redacted()
		}
	}

	if strings.Contains(rest, "@") {
		return scheme + "://" + redactUserinfo(rest)
	}

	if err != nil {
		return scheme + "://[redacted]"
	}

	return dsn
}

// redactUserinfo hides the password in a user:pass@rest string, the form a
// go-sql-driver MySQL DSN uses (user:pass@tcp(host:3306)/db). It cuts at the
// last '@' so a password containing '@' is hidden in full; an '@' later in
// the string can only make it hide more than needed, never less.
func redactUserinfo(s string) string {
	at := strings.LastIndex(s, "@")
	if at < 0 {
		return s
	}

	user, _, hasPassword := strings.Cut(s[:at], ":")
	if !hasPassword {
		return s
	}

	return user + ":" + redactedSecret + s[at:]
}

// redactSensitiveParams hides the value of every credential-bearing key=value
// pair in s.
func redactSensitiveParams(s string) string {
	return sensitiveDSNParam.ReplaceAllString(s, "${1}${2}${3}"+redactedSecret)
}
