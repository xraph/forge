package typescript

import "testing"

func TestToCamel(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user_id", "userId"},
		{"user-id", "userId"},
		{"userId", "userId"},   // already camel: must be preserved, not lowercased
		{"UserID", "userID"},   // leading cap dropped, interior caps kept
		{"id", "id"},
		{"", ""},
		{"_", "_"},   // separators only: no words found, name returned unchanged, no panic
		{"--", "--"}, // separators only: no words found, name returned unchanged, no panic
	}

	for _, c := range cases {
		if got := toCamel(c.in); got != c.want {
			t.Errorf("toCamel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestToPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user_id", "UserId"},
		{"message.created", "MessageCreated"},
		{"userId", "UserId"},
		{"", ""},
		{"_", ""},
		{"--", ""},
	}

	for _, c := range cases {
		if got := toPascal(c.in); got != c.want {
			t.Errorf("toPascal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
