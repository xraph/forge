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

	// csrfMaxAge is the maximum age of a valid CSRF token.
	csrfMaxAge = 1 * time.Hour

	// csrfTokenSeparator separates the HMAC signature from the timestamp in a token.
	csrfTokenSeparator = "."
)

// CSRFManager handles CSRF token generation and validation.
// Tokens are produced as HMAC-SHA256(secret, timestamp) + "." + timestamp,
// where timestamp is the Unix epoch in seconds. Tokens older than one hour
// are rejected during validation.
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

// GenerateToken creates a new CSRF token. The token encodes the current
// timestamp and an HMAC signature so that it can be verified later without
// server-side storage.
//
// Token format: hex(HMAC-SHA256(secret, timestamp)) + "." + timestamp
func (m *CSRFManager) GenerateToken() string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := m.computeHMAC(timestamp)

	return signature + csrfTokenSeparator + timestamp
}

// ValidateToken validates a CSRF token. A token is valid when:
//  1. It is well-formed (contains exactly one separator).
//  2. The HMAC signature matches the recomputed value.
//  3. The embedded timestamp is no older than csrfMaxAge (1 hour).
func (m *CSRFManager) ValidateToken(token string) bool {
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

	expected := m.computeHMAC(timestamp)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// computeHMAC produces a hex-encoded HMAC-SHA256 of the given message
// using the manager's secret. The caller must hold at least a read lock on mu.
func (m *CSRFManager) computeHMAC(message string) string {
	mac := hmac.New(sha256.New, m.secret)
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
