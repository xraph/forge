package typescript

import (
	"strings"
	"testing"
)

// errorsMentioning returns the subset of errs containing needle.
func errorsMentioning(errs []string, needle string) []string {
	var out []string

	for _, e := range errs {
		if strings.Contains(e, needle) {
			out = append(out, e)
		}
	}

	return out
}

func TestNoDanglingAuthConfig(t *testing.T) {
	for _, f := range gateFixtures() {
		t.Run(f.Name, func(t *testing.T) {
			errs := typeCheck(t, generateTo(t, f))

			if bad := errorsMentioning(errs, "AuthConfig"); len(bad) > 0 {
				t.Errorf("AuthConfig is referenced but not exported:\n%s", strings.Join(bad, "\n"))
			}
		})
	}
}
