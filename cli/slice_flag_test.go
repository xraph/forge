package cli

import "testing"

// TestStringSliceFlagParsing pins both forms the flag has always claimed to
// support and previously supported neither of: comma separation within one
// occurrence, and accumulation across several.
//
// The old behaviour wrapped the value whole and let each occurrence replace
// the last, so `--exclude /a --exclude /b` silently generated a client that
// still contained everything under /a.
func TestStringSliceFlagParsing(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"single", "/api", []string{"/api"}},
		{"comma separated", "/api,/identity", []string{"/api", "/identity"}},
		{"spaces are trimmed", "/api, /identity", []string{"/api", "/identity"}},
		{"empty segments dropped", "/api,,", []string{"/api"}},
		{"empty", "", []string{}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseValue(tc.in, StringSliceFlagType)
			if err != nil {
				t.Fatalf("parseValue: %v", err)
			}

			values, ok := got.([]string)
			if !ok {
				t.Fatalf("parseValue returned %T, want []string", got)
			}

			if len(values) != len(tc.want) {
				t.Fatalf("parseValue(%q) = %v, want %v", tc.in, values, tc.want)
			}

			for i := range tc.want {
				if values[i] != tc.want[i] {
					t.Errorf("parseValue(%q) = %v, want %v", tc.in, values, tc.want)

					break
				}
			}
		})
	}
}

// TestStringSliceFlagValueSplitsRawString covers the accessor's own fallback,
// which splits a raw string that never went through parseValue.
func TestStringSliceFlagValueSplitsRawString(t *testing.T) {
	fv := &flagValue{rawValue: "/api,/identity", isSet: true}

	got := fv.StringSlice()
	if len(got) != 2 || got[0] != "/api" || got[1] != "/identity" {
		t.Errorf("StringSlice() = %v, want [/api /identity]", got)
	}
}
