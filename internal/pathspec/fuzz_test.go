package pathspec

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func FuzzParseRenderRoundTrip(f *testing.F) {
	seeds := []string{
		"/",
		"//",
		"/users",
		"/users/:id",
		"/users/{id}",
		"/users/{id:int}",
		"/users/{id:uuid}",
		"/invoices/{status:enum(draft|sent|paid)}",
		"/files/*",
		"/files/*path",
		"/{org}/repos/{repo}/files/*",
		"/users/",
		"/a{b",
		"/users/:",
		"/api/*/assets",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		p, err := Parse(raw)
		if err != nil {
			return // rejection is a valid outcome; the property is about accepted input
		}

		for _, syntax := range []Syntax{SyntaxColon, SyntaxBrace, SyntaxOpenAPI} {
			once := p.Render(syntax)

			again, err := Parse(once)
			require.NoErrorf(t, err, "Render(%d) produced %q from %q, which does not parse", syntax, once, raw)

			require.Equalf(t, once, again.Render(syntax),
				"Render(%d) is not stable: %q rendered %q then %q", syntax, raw, once, again.Render(syntax))
		}
	})
}
