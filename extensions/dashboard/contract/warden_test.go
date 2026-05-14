package contract

import (
	"context"
	"errors"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

type stubWarden struct {
	allow      bool
	redactions []string
}

func (s *stubWarden) Authorize(_ context.Context, _ Principal, _ Action) (Decision, error) {
	return Decision{Allow: s.allow, Redactions: s.redactions}, nil
}

func TestWardenRegistry_RegisterAndGet(t *testing.T) {
	r := NewWardenRegistry()
	w := &stubWarden{allow: true}
	if err := r.Register("tenantOwner", w); err != nil {
		t.Fatalf("register: %v", err)
	}
	got, ok := r.Get("tenantOwner")
	if !ok || got != w {
		t.Error("registered warden not found")
	}
}

func TestWardenRegistry_DuplicateName_Fails(t *testing.T) {
	r := NewWardenRegistry()
	_ = r.Register("x", &stubWarden{allow: true})
	if err := r.Register("x", &stubWarden{allow: false}); err == nil {
		t.Error("duplicate registration should fail")
	}
}

func TestWardenRegistry_MissingName_NotOK(t *testing.T) {
	r := NewWardenRegistry()
	if _, ok := r.Get("nope"); ok {
		t.Error("missing warden should not be found")
	}
}

func TestPrincipal_FromUserInfo(t *testing.T) {
	user := &dashauth.UserInfo{Subject: "u1", Roles: []string{"admin"}}
	p := PrincipalFor(user)
	if p.User != user {
		t.Error("principal should hold the user")
	}
}

func TestDecision_DenialPropagates(t *testing.T) {
	w := &stubWarden{allow: false}
	d, err := w.Authorize(context.Background(), Principal{}, Action{})
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	if d.Allow {
		t.Error("expected deny")
	}
	_ = errors.New
}
