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
		{"...", "..."},   // separators only, no panic
		{"_a_", "a"},
		{"123abc", "123abc"}, // leading digit: no case boundary, no panic
		{"Ábc", "ábc"},       // multi-byte leading rune must be lowercased by rune, not by byte
		{"ábc", "ábc"},       // already-lowercase multi-byte leading rune: unchanged
		{"café_id", "caféId"}, // multi-byte rune mid-word must survive untouched
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
		{"...", ""},
		{"_a_", "A"},
		{"123abc", "123abc"},  // leading digit: ToUpper is a no-op, no panic
		{"ábc_id", "ÁbcId"},   // multi-byte leading rune must be uppercased by rune, not by byte
	}

	for _, c := range cases {
		if got := toPascal(c.in); got != c.want {
			t.Errorf("toPascal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
