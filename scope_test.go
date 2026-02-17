package forge_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xraph/forge"
)

// --- Scope constructors and getters ---

func TestNewAppScope(t *testing.T) {
	s := forge.NewAppScope("app_01h9a1b2c3")

	assert.Equal(t, "app_01h9a1b2c3", s.AppID())
	assert.Equal(t, "", s.OrgID())
	assert.Equal(t, forge.ScopeApp, s.Level())
	assert.False(t, s.HasOrg())
	assert.False(t, s.IsZero())
}

func TestNewOrgScope(t *testing.T) {
	s := forge.NewOrgScope("app_01h9a1b2c3", "org_01h9a1b2c4")

	assert.Equal(t, "app_01h9a1b2c3", s.AppID())
	assert.Equal(t, "org_01h9a1b2c4", s.OrgID())
	assert.Equal(t, forge.ScopeOrganization, s.Level())
	assert.True(t, s.HasOrg())
	assert.False(t, s.IsZero())
}

func TestScope_IsZero(t *testing.T) {
	var s forge.Scope
	assert.True(t, s.IsZero())
}

func TestScope_String(t *testing.T) {
	tests := []struct {
		name  string
		scope forge.Scope
		want  string
	}{
		{"app only", forge.NewAppScope("app_123"), "app_123"},
		{"app and org", forge.NewOrgScope("app_123", "org_456"), "app_123/org_456"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.scope.String())
		})
	}
}

func TestScope_Key(t *testing.T) {
	s := forge.NewOrgScope("app_123", "org_456")

	assert.Equal(t, "app_123", s.Key(forge.ScopeApp))
	assert.Equal(t, "org_456", s.Key(forge.ScopeOrganization))
}

func TestScope_Key_PanicsWithoutOrg(t *testing.T) {
	s := forge.NewAppScope("app_123")

	assert.Panics(t, func() {
		s.Key(forge.ScopeOrganization)
	})
}

func TestScope_Key_PanicsInvalidLevel(t *testing.T) {
	s := forge.NewAppScope("app_123")

	assert.Panics(t, func() {
		s.Key(forge.ScopeLevel(99))
	})
}

func TestScope_IsValueType(t *testing.T) {
	s1 := forge.NewOrgScope("a", "b")
	s2 := s1 // value copy
	assert.Equal(t, s1.AppID(), s2.AppID())
	assert.Equal(t, s1.OrgID(), s2.OrgID())
}

func TestScopeLevel_String(t *testing.T) {
	assert.Equal(t, "app", forge.ScopeApp.String())
	assert.Equal(t, "organization", forge.ScopeOrganization.String())
	assert.Equal(t, "unknown", forge.ScopeLevel(99).String())
}

// --- stdlib context.Context accessors ---

func TestWithScope_ScopeFrom(t *testing.T) {
	ctx := context.Background()
	s := forge.NewOrgScope("app_1", "org_2")
	ctx = forge.WithScope(ctx, s)

	got, ok := forge.ScopeFrom(ctx)
	require.True(t, ok)
	assert.Equal(t, "app_1", got.AppID())
	assert.Equal(t, "org_2", got.OrgID())
}

func TestScopeFrom_Missing(t *testing.T) {
	_, ok := forge.ScopeFrom(context.Background())
	assert.False(t, ok)
}

func TestMustScope_Success(t *testing.T) {
	ctx := forge.WithScope(context.Background(), forge.NewAppScope("x"))
	s := forge.MustScope(ctx)
	assert.Equal(t, "x", s.AppID())
}

func TestMustScope_Panics(t *testing.T) {
	assert.Panics(t, func() {
		forge.MustScope(context.Background())
	})
}

func TestAppIDFrom(t *testing.T) {
	ctx := forge.WithScope(context.Background(), forge.NewOrgScope("a", "o"))
	assert.Equal(t, "a", forge.AppIDFrom(ctx))
}

func TestAppIDFrom_Missing(t *testing.T) {
	assert.Equal(t, "", forge.AppIDFrom(context.Background()))
}

func TestOrgIDFrom(t *testing.T) {
	ctx := forge.WithScope(context.Background(), forge.NewOrgScope("a", "o"))
	assert.Equal(t, "o", forge.OrganizationIDFrom(ctx))
}

func TestOrgIDFrom_Missing(t *testing.T) {
	assert.Equal(t, "", forge.OrganizationIDFrom(context.Background()))
}

// --- Error sentinels ---

func TestErrNoScope(t *testing.T) {
	assert.Error(t, forge.ErrNoScope)
	assert.Contains(t, forge.ErrNoScope.Error(), "scope")
}

func TestErrNoOrg(t *testing.T) {
	assert.Error(t, forge.ErrNoOrg)
	assert.Contains(t, forge.ErrNoOrg.Error(), "organization")
}
