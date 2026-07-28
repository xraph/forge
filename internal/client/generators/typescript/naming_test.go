package typescript

import "testing"

// TestToCamel is table-driven and covers, in one place:
//   - the defect this file fixes: a run of capitals (an acronym) not being
//     split from a following capitalised word (HTTPStatus), and an all-caps
//     word being mangled to one-lower-rune-plus-caps (uSERID) instead of
//     being normalised as a whole acronym (userId).
//   - the pre-existing regression guards that must keep passing.
//
// Decision (see naming.go's isAllUpper/lowerFirst/upperFirst doc comments for
// the full rationale): a word that is entirely uppercase is treated as an
// acronym and normalised as a whole — lowercased entirely as the leading
// word, title-cased (first rune up, rest down) as a trailing word. This is
// why toCamel("userID") now yields "userId" rather than the old "userID":
// once HTTPStatus -> httpStatus normalises "HTTP" down to "http", leaving
// "ID" as "ID" in userID would be the one inconsistent case. toCamel and
// toPascal agree on this: both flatten an all-caps word instead of leaving
// interior acronyms shouting.
func TestToCamel(t *testing.T) {
	cases := []struct{ in, want string }{
		// Regression guards: already-correct behaviour that must not change.
		{"user_id", "userId"},
		{"user-id", "userId"},
		{"userId", "userId"}, // already camel: must be preserved, not lowercased
		{"id", "id"},
		{"a", "a"},
		{"", ""},
		{"_", "_"},     // separators only: no words found, name returned unchanged, no panic
		{"--", "--"},   // separators only: no words found, name returned unchanged, no panic
		{"...", "..."}, // separators only, no panic
		{"_a_", "a"},
		{"123abc", "123abc"},  // leading digit: no case boundary, no panic
		{"Ábc", "ábc"},        // multi-byte leading rune must be lowercased by rune, not by byte
		{"ábc", "ábc"},        // already-lowercase multi-byte leading rune: unchanged
		{"café_id", "caféId"}, // multi-byte rune mid-word must survive untouched

		// The defect: a run of capitals must split before a following
		// capitalised word, and an all-caps word must be normalised as a
		// whole rather than having only its first rune changed.
		{"USER_ID", "userId"},
		{"HTTPStatus", "httpStatus"},
		{"HTTP_STATUS_CODE", "httpStatusCode"},
		{"ID", "id"},
		{"A", "a"},

		// Judgment call: an all-caps trailing acronym now normalises like any
		// other all-caps word, so this changes from the old "userID".
		{"userID", "userId"},
		{"UserID", "userId"},
	}

	for _, c := range cases {
		if got := toCamel(c.in); got != c.want {
			t.Errorf("toCamel(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestToPascal mirrors TestToCamel's cases in PascalCase, plus its own
// pre-existing regression guards. toPascal("HTTPStatus") -> "HttpStatus" (not
// "HTTPStatus") for the same reason toCamel flattens "userID" -> "userId":
// an all-caps word is an acronym to be normalised as a whole, consistently,
// wherever it appears in the identifier.
func TestToPascal(t *testing.T) {
	cases := []struct{ in, want string }{
		// Regression guards.
		{"user_id", "UserId"},
		{"message.created", "MessageCreated"},
		{"userId", "UserId"},
		{"", ""},
		{"_", ""},
		{"--", ""},
		{"...", ""},
		{"_a_", "A"},
		{"123abc", "123abc"}, // leading digit: ToUpper is a no-op, no panic
		{"ábc_id", "ÁbcId"},  // multi-byte leading rune must be uppercased by rune, not by byte

		// The defect, mirrored in PascalCase.
		{"USER_ID", "UserId"},
		{"HTTPStatus", "HttpStatus"},
		{"HTTP_STATUS_CODE", "HttpStatusCode"},
		{"ID", "Id"},
		{"A", "A"},
		{"userID", "UserId"},
	}

	for _, c := range cases {
		if got := toPascal(c.in); got != c.want {
			t.Errorf("toPascal(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestToSnake covers snake_case conversion, in particular that it stays
// consistent with toCamel/toPascal's splitWords-driven acronym handling:
// "HTTPStatus" must split into "HTTP" + "Status" here too, giving
// "http_status" rather than "h_t_t_p_status" or "httpstatus".
func TestToSnake(t *testing.T) {
	cases := []struct{ in, want string }{
		{"user_id", "user_id"},
		{"userId", "user_id"},
		{"UserId", "user_id"},
		{"user-id", "user_id"},
		{"message.created", "message_created"},
		{"id", "id"},
		{"", ""},
		{"_", "_"},
		{"USER_ID", "user_id"},
		{"HTTPStatus", "http_status"},
		{"HTTP_STATUS_CODE", "http_status_code"},
		{"ID", "id"},
		{"userID", "user_id"},
	}

	for _, c := range cases {
		if got := toSnake(c.in); got != c.want {
			t.Errorf("toSnake(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
