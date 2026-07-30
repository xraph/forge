package shared

import (
	"net/http"
	"testing"
)

// A 5xx body built from a wrapped error embeds the error text. Both the router's
// fallback and DefaultErrorHandler emit these bodies, so the redaction lives here
// where both go through it.
func TestSanitizeErrorBodyRedactsServerErrors(t *testing.T) {
	t.Cleanup(func() { SetExposeInternalErrors(false) })

	leaky := map[string]any{
		"code":    500,
		"details": `pq: relation "internal_billing" does not exist`,
	}

	SetExposeInternalErrors(false)

	got, ok := SanitizeErrorBody(http.StatusInternalServerError, leaky).(map[string]any)
	if !ok {
		t.Fatalf("sanitized body has unexpected type %T", got)
	}

	if _, present := got["details"]; present {
		t.Errorf("server-error detail survived redaction: %v", got)
	}

	if got["code"] != http.StatusInternalServerError {
		t.Errorf("status code lost: %v", got["code"])
	}

	// 4xx bodies are actionable by the client and must pass through untouched.
	clientErr := map[string]any{"code": 400, "details": "field 'email' is required"}
	if out := SanitizeErrorBody(http.StatusBadRequest, clientErr); out == nil {
		t.Error("client-error body was dropped")
	} else if m, _ := out.(map[string]any); m["details"] != "field 'email' is required" {
		t.Errorf("client-error detail was altered: %v", m)
	}

	// With exposure on, 5xx detail is preserved for local debugging.
	SetExposeInternalErrors(true)

	if out, _ := SanitizeErrorBody(http.StatusInternalServerError, leaky).(map[string]any); out["details"] == nil {
		t.Error("exposure enabled but detail was still redacted")
	}
}
