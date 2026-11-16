package security

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestCSRFToken_BoundToIdentity(t *testing.T) {
	m := NewCSRFManager()

	alice := m.GenerateTokenFor("alice")
	if !m.ValidateTokenFor(alice, "alice") {
		t.Fatal("token does not validate for the identity it was minted for")
	}

	if m.ValidateTokenFor(alice, "bob") {
		t.Error("token minted for alice validated for bob")
	}

	if m.ValidateTokenFor(alice, "") {
		t.Error("token minted for alice validated for an anonymous caller")
	}

	// The case the review reproduced: an attacker mints an anonymous token
	// and replays it in a request carrying the victim's identity.
	anon := m.GenerateToken()
	if m.ValidateTokenFor(anon, "victim") {
		t.Error("anonymous token validated for a signed-in identity")
	}

	if !m.ValidateToken(anon) {
		t.Error("anonymous token no longer validates anonymously")
	}
}

// The identity is length-prefixed in the HMAC input, so moving bytes between
// identity and timestamp cannot forge another identity's signature.
func TestCSRFToken_IdentityCannotBleedIntoTimestamp(t *testing.T) {
	m := NewCSRFManager()
	tok := m.GenerateTokenFor("a")

	sig, ts, _ := strings.Cut(tok, ".")
	if m.ValidateTokenFor(sig+"."+ts, "a|") {
		t.Error("identity with a separator suffix validated")
	}
}

func TestCSRFToken_ExpiresAtMaxAge(t *testing.T) {
	m := NewCSRFManager()
	if m.MaxAge() != time.Hour {
		t.Fatalf("MaxAge = %v, want 1h", m.MaxAge())
	}

	old := strconv.FormatInt(time.Now().Add(-m.MaxAge()-time.Minute).Unix(), 10)
	m.mu.RLock()
	stale := m.computeHMAC("alice", old) + "." + old
	m.mu.RUnlock()

	if m.ValidateTokenFor(stale, "alice") {
		t.Error("token older than MaxAge validated")
	}
}
