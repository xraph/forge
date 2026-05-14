package contract

import (
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

func u(roles, scopes []string) *dashauth.UserInfo {
	return &dashauth.UserInfo{Roles: roles, Scopes: scopes}
}

func TestPredicate_Empty_Allows(t *testing.T) {
	if !(&Predicate{}).Allow(u(nil, nil), nil) {
		t.Error("empty predicate should allow")
	}
}

func TestPredicate_AllRequires(t *testing.T) {
	p := &Predicate{All: []string{"role:admin", "scope:users.write"}}
	if !p.Allow(u([]string{"admin"}, []string{"users.write"}), nil) {
		t.Error("admin+users.write should pass all")
	}
	if p.Allow(u([]string{"admin"}, nil), nil) {
		t.Error("missing scope should fail all")
	}
}

func TestPredicate_AnyRequires(t *testing.T) {
	p := &Predicate{Any: []string{"role:admin", "role:owner"}}
	if !p.Allow(u([]string{"owner"}, nil), nil) {
		t.Error("owner alone should pass any")
	}
	if p.Allow(u([]string{"viewer"}, nil), nil) {
		t.Error("neither admin nor owner should fail any")
	}
}

func TestPredicate_NotForbids(t *testing.T) {
	p := &Predicate{Not: []string{"role:guest"}}
	if !p.Allow(u([]string{"admin"}, nil), nil) {
		t.Error("admin should pass not-guest")
	}
	if p.Allow(u([]string{"guest"}, nil), nil) {
		t.Error("guest should fail not-guest")
	}
}

func TestPredicate_AllAndAny_Combined(t *testing.T) {
	p := &Predicate{
		All: []string{"scope:users.read"},
		Any: []string{"role:admin", "role:owner"},
	}
	pass := u([]string{"owner"}, []string{"users.read"})
	fail := u([]string{"owner"}, nil)
	if !p.Allow(pass, nil) {
		t.Error("pass case failed")
	}
	if p.Allow(fail, nil) {
		t.Error("fail case allowed")
	}
}

func TestPermissionsHash_StableForEquivalentSlice(t *testing.T) {
	a := PermissionsHash(u([]string{"admin", "owner"}, []string{"x", "y"}))
	b := PermissionsHash(u([]string{"owner", "admin"}, []string{"y", "x"}))
	if a != b {
		t.Errorf("hash not stable across order: %s vs %s", a, b)
	}
}

func TestPermissionsHash_DiffersWhenRolesDiffer(t *testing.T) {
	a := PermissionsHash(u([]string{"admin"}, nil))
	b := PermissionsHash(u([]string{"viewer"}, nil))
	if a == b {
		t.Error("hash should differ for different roles")
	}
}
