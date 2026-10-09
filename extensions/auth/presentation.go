package auth

import (
	"context"
	"crypto/sha256"
	"strings"
	"unicode"
)

type explicitFrameCredentialKey struct{}

type explicitFrameCredential struct {
	scheme string
	digest [sha256.Size]byte
}

// ParseExplicitFrameCredential parses a protocol frame's selected credential.
// You can supply a bare token, Bearer <token>, or DPoP <token>. The returned
// scheme is canonical, and token bytes are preserved after splitting fields.
// Proofs must use the transport's existing proof channel.
func ParseExplicitFrameCredential(presentation string) (scheme, token string, err error) {
	if strings.IndexFunc(presentation, unicode.IsControl) >= 0 {
		return "", "", ErrInvalidCredentials
	}

	fields := strings.Fields(presentation)
	switch len(fields) {
	case 1:
		if strings.EqualFold(fields[0], "Bearer") || strings.EqualFold(fields[0], "DPoP") {
			return "", "", ErrInvalidCredentials
		}

		scheme, token = "Bearer", fields[0]
	case 2:
		scheme, token = fields[0], fields[1]
	default:
		return "", "", ErrInvalidCredentials
	}

	scheme, ok := explicitFrameScheme(scheme, token)
	if !ok {
		return "", "", ErrInvalidCredentials
	}

	return scheme, token, nil
}

// WithExplicitFrameCredential records a credential received in a protocol
// frame after request middleware. Call it only from trusted transport code,
// using the same parsed scheme and token you put in the Authorization header.
// Never create this marker from request headers, cookies, or claims.
//
// The marker stores a digest, preserves the parent context, and grants no
// identity. Providers must still validate the credential and any required proof.
func WithExplicitFrameCredential(ctx context.Context, scheme, token string) (context.Context, error) {
	scheme, ok := explicitFrameScheme(scheme, token)
	if !ok {
		return ctx, ErrInvalidCredentials
	}

	return context.WithValue(ctx, explicitFrameCredentialKey{}, explicitFrameCredential{
		scheme: scheme,
		digest: sha256.Sum256([]byte(token)),
	}), nil
}

// MatchesExplicitFrameCredential reports whether trusted transport code marked
// this exact scheme and token as an explicit frame presentation. Only check
// credentials extracted from a valid Authorization presentation. A match does
// not authenticate the token or promote credentials read from cookie fallback.
func MatchesExplicitFrameCredential(ctx context.Context, scheme, token string) bool {
	if ctx == nil {
		return false
	}

	scheme, ok := explicitFrameScheme(scheme, token)
	if !ok {
		return false
	}

	marker, ok := ctx.Value(explicitFrameCredentialKey{}).(explicitFrameCredential)

	return ok && marker.scheme == scheme && marker.digest == sha256.Sum256([]byte(token))
}

func explicitFrameScheme(scheme, token string) (string, bool) {
	if token == "" || strings.IndexFunc(token, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return "", false
	}

	switch {
	case strings.EqualFold(scheme, "Bearer"):
		return "Bearer", true
	case strings.EqualFold(scheme, "DPoP"):
		return "DPoP", true
	default:
		return "", false
	}
}
