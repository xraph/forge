package auth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/xraph/forge/extensions/auth"
)

func TestParseExplicitFrameCredential(t *testing.T) {
	for _, tt := range []struct {
		input, scheme, token string
	}{
		{"token", "Bearer", "token"},
		{"Bearer token", "Bearer", "token"},
		{"bEaReR CaseSensitive", "Bearer", "CaseSensitive"},
		{" DPoP   token ", "DPoP", "token"},
	} {
		t.Run(tt.input, func(t *testing.T) {
			scheme, token, err := auth.ParseExplicitFrameCredential(tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.scheme, scheme)
			require.Equal(t, tt.token, token)
		})
	}

	for _, input := range []string{"", " \t", "Bearer ", "DPoP", "Basic token", "DPoP token proof", "Bearer tok\x00en", "Bearer token\r\n", "Bearer\ttoken"} {
		_, _, err := auth.ParseExplicitFrameCredential(input)
		require.ErrorIs(t, err, auth.ErrInvalidCredentials)
	}
}

func TestExplicitFrameCredentialPreservesContextAndBindsPresentation(t *testing.T) {
	type proofKey struct{}

	parent, cancel := context.WithCancel(t.Context())
	defer cancel()

	identity := &auth.AuthContext{Subject: "existing-subject"}
	parent = auth.WithContext(context.WithValue(parent, proofKey{}, "existing-proof"), identity)
	ctx, err := auth.WithExplicitFrameCredential(parent, "bEaReR", "ExactToken")
	require.NoError(t, err)
	require.Equal(t, "existing-proof", ctx.Value(proofKey{}))
	got, ok := auth.FromContext(ctx)
	require.True(t, ok)
	require.Same(t, identity, got)
	require.True(t, auth.MatchesExplicitFrameCredential(ctx, "Bearer", "ExactToken"))
	require.False(t, auth.MatchesExplicitFrameCredential(ctx, "DPoP", "ExactToken"))
	require.False(t, auth.MatchesExplicitFrameCredential(ctx, "Bearer", "exactToken"))
	require.False(t, auth.MatchesExplicitFrameCredential(parent, "Bearer", "ExactToken"))

	cancel()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
}

func TestExplicitFrameCredentialRejectsInvalidPresentation(t *testing.T) {
	for _, tt := range []struct{ scheme, token string }{
		{"", "token"}, {"cookie", "token"}, {"Basic", "token"},
		{"Bearer", ""}, {"Bearer", " "}, {"Bearer", " token"},
		{"Bearer", "token other"}, {"DPoP", "token\r\n"},
	} {
		ctx := t.Context()
		marked, err := auth.WithExplicitFrameCredential(ctx, tt.scheme, tt.token)
		require.ErrorIs(t, err, auth.ErrInvalidCredentials)
		require.Same(t, ctx, marked)
		require.False(t, auth.MatchesExplicitFrameCredential(marked, tt.scheme, tt.token))
	}
}
