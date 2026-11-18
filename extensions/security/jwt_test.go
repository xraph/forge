package security

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/xraph/forge"
	forge_http "github.com/xraph/go-utils/http"
	logger "github.com/xraph/go-utils/log"
)

const testHMACKey = "0123456789abcdef0123456789abcdef"

func newTestJWTManager(t *testing.T, mutate func(*JWTConfig)) *JWTManager {
	t.Helper()

	cfg := DefaultJWTConfig()
	cfg.SigningKey = testHMACKey

	if mutate != nil {
		mutate(&cfg)
	}

	jm, err := NewJWTManager(cfg, logger.NewNoopLogger())
	if err != nil {
		t.Fatal(err)
	}

	return jm
}

func jwtMiddlewareStatus(t *testing.T, jm *JWTManager, token string) (int, *JWTClaims, *JWTClaims) {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/admin", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	w := httptest.NewRecorder()

	var fromForge, fromStd *JWTClaims

	_ = JWTMiddleware(jm)(func(ctx forge.Context) error {
		fromForge, _ = GetJWTClaims(ctx)
		fromStd, _ = GetJWTClaimsFromStdContext(ctx.Context())

		return ctx.String(http.StatusOK, "in")
	})(forge_http.NewContext(w, req, nil))

	return w.Code, fromForge, fromStd
}

// A 7-day refresh token used to pass the access-token middleware because both
// token kinds were indistinguishable.
func TestJWTMiddleware_RejectsRefreshToken(t *testing.T) {
	jm := newTestJWTManager(t, nil)

	refresh, err := jm.GenerateRefreshToken("user-1")
	if err != nil {
		t.Fatal(err)
	}

	if code, _, _ := jwtMiddlewareStatus(t, jm, refresh); code != http.StatusUnauthorized {
		t.Fatalf("refresh token as access token: status %d, want 401", code)
	}

	if _, err := jm.ValidateToken(refresh); !errors.Is(err, ErrJWTWrongType) {
		t.Fatalf("ValidateToken(refresh) = %v, want ErrJWTWrongType", err)
	}

	claims, err := jm.ValidateRefreshToken(refresh)
	if err != nil || claims.UserID != "user-1" {
		t.Fatalf("ValidateRefreshToken(refresh) = %v, %v", claims, err)
	}

	access, err := jm.GenerateToken(&JWTClaims{UserID: "user-1"})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := jm.ValidateRefreshToken(access); !errors.Is(err, ErrJWTWrongType) {
		t.Fatalf("ValidateRefreshToken(access) = %v, want ErrJWTWrongType", err)
	}

	code, fromForge, fromStd := jwtMiddlewareStatus(t, jm, access)
	if code != http.StatusOK {
		t.Fatalf("access token: status %d, want 200", code)
	}

	if fromForge == nil || fromStd == nil || fromStd.UserID != "user-1" {
		t.Fatalf("claims not published to both contexts: forge=%v std=%v", fromForge, fromStd)
	}
}

func signHMAC(t *testing.T, method jwt.SigningMethod, key string, claims jwt.Claims) string {
	t.Helper()

	s, err := jwt.NewWithClaims(method, claims).SignedString([]byte(key))
	if err != nil {
		t.Fatal(err)
	}

	return s
}

func TestJWTManager_ValidateTokenRequirements(t *testing.T) {
	jm := newTestJWTManager(t, func(c *JWTConfig) {
		c.Issuer = "forge-test"
		c.Audience = "api"
	})

	now := time.Now()
	valid := func() *JWTClaims {
		return &JWTClaims{
			UserID:    "u",
			TokenType: TokenTypeAccess,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "forge-test",
				Audience:  jwt.ClaimStrings{"api"},
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			},
		}
	}

	if _, err := jm.ValidateToken(signHMAC(t, jwt.SigningMethodHS256, testHMACKey, valid())); err != nil {
		t.Fatalf("baseline token rejected: %v", err)
	}

	noExp := valid()
	noExp.ExpiresAt = nil

	noType := valid()
	noType.TokenType = ""

	wrongIss := valid()
	wrongIss.Issuer = "someone-else"

	wrongAud := valid()
	wrongAud.Audience = jwt.ClaimStrings{"other"}

	expired := valid()
	expired.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))

	cases := map[string]string{
		"missing exp":     signHMAC(t, jwt.SigningMethodHS256, testHMACKey, noExp),
		"missing typ":     signHMAC(t, jwt.SigningMethodHS256, testHMACKey, noType),
		"wrong issuer":    signHMAC(t, jwt.SigningMethodHS256, testHMACKey, wrongIss),
		"wrong audience":  signHMAC(t, jwt.SigningMethodHS256, testHMACKey, wrongAud),
		"expired":         signHMAC(t, jwt.SigningMethodHS256, testHMACKey, expired),
		"other algorithm": signHMAC(t, jwt.SigningMethodHS512, testHMACKey, valid()),
		"other key":       signHMAC(t, jwt.SigningMethodHS256, "a-different-key-entirely-0000000", valid()),
	}

	for name, tok := range cases {
		if _, err := jm.ValidateToken(tok); err == nil {
			t.Errorf("%s: token accepted", name)
		}
	}
}

func TestJWTManager_SeparateRefreshKey(t *testing.T) {
	jm := newTestJWTManager(t, func(c *JWTConfig) {
		c.RefreshSigningKey = "refresh-only-key-0123456789abcdef"
	})

	refresh, _ := jm.GenerateRefreshToken("u")
	if _, err := jm.ValidateRefreshToken(refresh); err != nil {
		t.Fatalf("refresh token rejected: %v", err)
	}

	// A refresh-typed token signed with the access key is not a refresh token.
	forged := signHMAC(t, jwt.SigningMethodHS256, testHMACKey, &JWTClaims{
		UserID:           "u",
		TokenType:        TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))},
	})
	if _, err := jm.ValidateRefreshToken(forged); err == nil {
		t.Fatal("refresh token signed with the access key was accepted")
	}
}

func TestNewJWTManager_RejectsNone(t *testing.T) {
	cfg := DefaultJWTConfig()
	cfg.SigningMethod = "none"
	cfg.SigningKey = testHMACKey

	if _, err := NewJWTManager(cfg, logger.NewNoopLogger()); err == nil {
		t.Fatal("signing method none was accepted")
	}
}

func pemEncode(t *testing.T, typ string, der []byte) string {
	t.Helper()

	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}))
}

func pkcs8PEM(t *testing.T, key any) string {
	t.Helper()

	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return pemEncode(t, "PRIVATE KEY", der)
}

func pkixPEM(t *testing.T, key any) string {
	t.Helper()

	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}

	return pemEncode(t, "PUBLIC KEY", der)
}

// RS*, ES* and EdDSA used to receive the configured key as []byte, so every
// token failed to sign or verify. Keys are now parsed from PEM.
func TestNewJWTManager_AsymmetricKeys(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	p256, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	p384, _ := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	edPub, edPriv, _ := ed25519.GenerateKey(rand.Reader)

	cases := []struct {
		method string
		priv   string
		pub    string
	}{
		{"RS256", pkcs8PEM(t, rsaKey), pkixPEM(t, &rsaKey.PublicKey)},
		{"PS256", pkcs8PEM(t, rsaKey), pkixPEM(t, &rsaKey.PublicKey)},
		{"ES256", pkcs8PEM(t, p256), pkixPEM(t, &p256.PublicKey)},
		{"ES384", pkcs8PEM(t, p384), pkixPEM(t, &p384.PublicKey)},
		{"EdDSA", pkcs8PEM(t, edPriv), pkixPEM(t, edPub)},
	}

	for _, c := range cases {
		signer, err := NewJWTManager(JWTConfig{SigningMethod: c.method, SigningKey: c.priv}, logger.NewNoopLogger())
		if err != nil {
			t.Fatalf("%s: %v", c.method, err)
		}

		tok, err := signer.GenerateToken(&JWTClaims{UserID: "u"})
		if err != nil {
			t.Fatalf("%s: sign: %v", c.method, err)
		}

		if _, err := signer.ValidateToken(tok); err != nil {
			t.Fatalf("%s: signer rejected its own token: %v", c.method, err)
		}

		verifier, err := NewJWTManager(JWTConfig{SigningMethod: c.method, PublicKey: c.pub}, logger.NewNoopLogger())
		if err != nil {
			t.Fatalf("%s: verify-only manager: %v", c.method, err)
		}

		if _, err := verifier.ValidateToken(tok); err != nil {
			t.Fatalf("%s: verify-only manager rejected the token: %v", c.method, err)
		}

		if _, err := verifier.GenerateToken(&JWTClaims{UserID: "u"}); err == nil {
			t.Fatalf("%s: verify-only manager signed a token", c.method)
		}
	}

	bad := []JWTConfig{
		{SigningMethod: "RS256", SigningKey: "not a pem key"},
		{SigningMethod: "RS256"},
		{SigningMethod: "ES256", SigningKey: pkcs8PEM(t, p384)},
		{SigningMethod: "ES256", PublicKey: pkixPEM(t, &rsaKey.PublicKey)},
		{SigningMethod: "EdDSA", SigningKey: pkcs8PEM(t, rsaKey)},
	}

	for i, cfg := range bad {
		if _, err := NewJWTManager(cfg, logger.NewNoopLogger()); err == nil {
			t.Errorf("bad config %d (%s) was accepted", i, cfg.SigningMethod)
		}
	}
}
