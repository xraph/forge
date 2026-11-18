package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// csrfSecretSize is the number of random bytes used for the HMAC secret.
	csrfSecretSize = 32

	// csrfMaxAge is the maximum age of a valid CSRF token. The token endpoint
	// advertises the same value as its expiry.
	csrfMaxAge = 1 * time.Hour

	// csrfTokenSeparator separates the HMAC signature from the timestamp in a token.
	csrfTokenSeparator = "."
)

// CSRFManager handles CSRF token generation and validation.
//
// A token is HMAC-SHA256(secret, identity, timestamp) + "." + timestamp,
// where identity is the caller the token was minted for (the signed-in
// user's subject, or empty for an anonymous caller) and timestamp is the
// Unix epoch in seconds. Binding the identity in means a token minted by one
// caller does not validate for another, so an attacker cannot fetch a token
// for themselves and replay it inside a request the victim's browser sends.
// Tokens older than MaxAge are rejected.
type CSRFManager struct {
	secret []byte
	mu     sync.RWMutex
}

// NewCSRFManager creates a new CSRF manager with a cryptographically random secret.
func NewCSRFManager() *CSRFManager {
	secret := make([]byte, csrfSecretSize)
	if _, err := rand.Read(secret); err != nil {
		panic(fmt.Sprintf("security: failed to generate CSRF secret: %v", err))
	}

	return &CSRFManager{
		secret: secret,
	}
}

// MaxAge is how long a token stays valid after it is minted. Anything that
// tells a client when its token expires should report this, so the client
// refreshes before validation starts refusing it.
func (m *CSRFManager) MaxAge() time.Duration {
	return csrfMaxAge
}

// GenerateToken mints a token for an anonymous caller. It is
// GenerateTokenFor(""), kept for callers that have no identity to bind.
func (m *CSRFManager) GenerateToken() string {
	return m.GenerateTokenFor("")
}

// GenerateTokenFor mints a token bound to identity, normally the signed-in
// user's subject. The token only validates through ValidateTokenFor with the
// same identity.
func (m *CSRFManager) GenerateTokenFor(identity string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := m.computeHMAC(identity, timestamp)

	return signature + csrfTokenSeparator + timestamp
}

// ValidateToken validates a token minted for an anonymous caller. It is
// ValidateTokenFor(token, "").
func (m *CSRFManager) ValidateToken(token string) bool {
	return m.ValidateTokenFor(token, "")
}

// ValidateTokenFor validates a token for identity. A token is valid when:
//  1. It is well-formed (contains exactly one separator).
//  2. The embedded timestamp is no older than MaxAge and not in the future.
//  3. The HMAC over identity and timestamp matches, so a token minted for a
//     different identity fails.
func (m *CSRFManager) ValidateTokenFor(token, identity string) bool {
	parts := strings.SplitN(token, csrfTokenSeparator, 2)
	if len(parts) != 2 {
		return false
	}

	signature := parts[0]
	timestamp := parts[1]

	// Verify the timestamp is a valid integer and within the allowed age.
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}

	age := time.Since(time.Unix(ts, 0))
	if age < 0 || age > csrfMaxAge {
		return false
	}

	// Recompute HMAC and compare in constant time.
	m.mu.RLock()
	defer m.mu.RUnlock()

	expected := m.computeHMAC(identity, timestamp)

	return hmac.Equal([]byte(signature), []byte(expected))
}

// computeHMAC produces a hex-encoded HMAC-SHA256 over identity and
// timestamp using the manager's secret. The identity is length-prefixed so
// no identity and timestamp pair can be re-split into a different pair that
// signs the same bytes. The caller must hold at least a read lock on mu.
func (m *CSRFManager) computeHMAC(identity, timestamp string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(strconv.Itoa(len(identity))))
	mac.Write([]byte{':'})
	mac.Write([]byte(identity))
	mac.Write([]byte{'|'})
	mac.Write([]byte(timestamp))

	return hex.EncodeToString(mac.Sum(nil))
}
