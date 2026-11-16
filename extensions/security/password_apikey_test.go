package security

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

// Malformed argon2id hashes used to panic inside argon2.IDKey (an empty
// digest crashed with a nil pointer, zero parallelism or iterations panic
// outright). Verify must return an error instead, and never true.
func TestPasswordHasher_VerifyRejectsMalformedArgon2(t *testing.T) {
	ph := NewPasswordHasher(DefaultPasswordHasherConfig())

	hashes := map[string]string{
		"empty digest":     "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$",
		"zero parallelism": "$argon2id$v=19$m=65536,t=3,p=0$c2FsdHNhbHQ$c29tZWRpZ2VzdHNvbWVkaWdlc3Q",
		"zero iterations":  "$argon2id$v=19$m=65536,t=0,p=4$c2FsdHNhbHQ$c29tZWRpZ2VzdHNvbWVkaWdlc3Q",
		"memory too small": "$argon2id$v=19$m=8,t=3,p=4$c2FsdHNhbHQ$c29tZWRpZ2VzdHNvbWVkaWdlc3Q",
		"memory too large": "$argon2id$v=19$m=4294967295,t=1,p=1$c2FsdHNhbHQ$c29tZWRpZ2VzdHNvbWVkaWdlc3Q",
		"short salt":       "$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$c29tZWRpZ2VzdHNvbWVkaWdlc3Q",
		"three-byte tag":   "$argon2id$v=19$m=65536,t=3,p=4$c2FsdHNhbHQ$YWJj",
		"bad base64":       "$argon2id$v=19$m=65536,t=3,p=4$!!!$c29tZWRpZ2VzdHNvbWVkaWdlc3Q",
		"missing fields":   "$argon2id$v=19$m=65536,t=3,p=4",
	}

	for name, hash := range hashes {
		ok, err := ph.Verify("anything", hash)
		if ok {
			t.Errorf("%s: Verify returned true", name)
		}

		if !errors.Is(err, ErrInvalidHash) {
			t.Errorf("%s: err = %v, want ErrInvalidHash", name, err)
		}
	}

	// A real hash still round-trips.
	h, err := ph.Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}

	if ok, err := ph.Verify("correct horse", h); !ok || err != nil {
		t.Fatalf("Verify(own hash) = %v, %v", ok, err)
	}

	if ok, _ := ph.Verify("wrong", h); ok {
		t.Fatal("wrong password verified")
	}
}

func TestAPIKeyManager_DeleteShortKeyDoesNotPanic(t *testing.T) {
	m := NewAPIKeyManager(DefaultAPIKeyConfig(), logger.NewNoopLogger())

	for _, key := range []string{"", "a", "short", "1234567"} {
		_, _ = m.CreateAPIKey(&APIKeyInfo{Key: key + "x"})
		m.DeleteAPIKey(key) // used to panic slicing key[:8]
	}

	if got := redactAPIKey("abcdefghijklmnopqrstuvwxyz"); got != "abcdef..." {
		t.Fatalf("redactAPIKey(long) = %q", got)
	}

	if got := redactAPIKey("abcdefgh"); got != "[redacted]" {
		t.Fatalf("redactAPIKey(short) = %q", got)
	}
}

func TestAPIKeyMiddleware_LookupAndStdContext(t *testing.T) {
	cfg := DefaultAPIKeyConfig()
	cfg.KeyLookup = "header:Authorization, query:api_key"
	cfg.KeyPrefix = "ApiKey"
	m := NewAPIKeyManager(cfg, logger.NewNoopLogger())

	key, err := m.CreateAPIKey(&APIKeyInfo{Name: "svc", UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}

	for name, req := range map[string]*http.Request{
		"header": func() *http.Request {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
			r.Header.Set("Authorization", "ApiKey "+key)

			return r
		}(),
		"query": httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/?api_key="+key, nil),
	} {
		var fromStd *APIKeyInfo

		w := httptest.NewRecorder()
		_ = APIKeyMiddleware(m)(func(ctx forge.Context) error {
			fromStd, _ = GetAPIKeyInfoFromStdContext(ctx.Context())

			return ctx.String(http.StatusOK, "ok")
		})(forge_http.NewContext(w, req, nil))

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", name, w.Code)
		}

		if fromStd == nil || fromStd.UserID != "u1" {
			t.Fatalf("%s: key info missing from the request context", name)
		}
	}
}
