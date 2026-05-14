// graph_test.go
package contract

import (
	"context"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

func TestBuildGraph_FiltersHiddenNodes(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /users
    intent: page.shell
    slots:
      main:
        - intent: resource.list
          slots:
            rowActions:
              - { intent: action.button, op: user.disable,
                  visibleWhen: { all: ["role:admin"] } }
              - { intent: action.button, op: user.view }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	build := NewGraphBuilder(r, NewWardenRegistry())

	got, err := build.Build(context.Background(), "users", "/users",
		Principal{User: &dashauth.UserInfo{Subject: "u1", Roles: []string{"viewer"}}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	actions := got.Slots["main"][0].Slots["rowActions"]
	if len(actions) != 1 {
		t.Fatalf("expected 1 action visible to viewer, got %d", len(actions))
	}
	if actions[0].Op != "user.view" {
		t.Errorf("wrong action remained: %+v", actions[0])
	}
}

func TestBuildGraph_AdminSeesAll(t *testing.T) {
	r := NewRegistry()
	m := mustManifest(t, `
schemaVersion: 1
contributor: { name: users, envelope: { supports: [v1], preferred: v1 } }
intents: []
graph:
  - route: /users
    intent: page.shell
    slots:
      main:
        - intent: resource.list
          slots:
            rowActions:
              - { intent: action.button, op: user.disable,
                  visibleWhen: { all: ["role:admin"] } }
`)
	if err := r.Register(m); err != nil {
		t.Fatalf("register: %v", err)
	}
	build := NewGraphBuilder(r, NewWardenRegistry())
	got, err := build.Build(context.Background(), "users", "/users",
		Principal{User: &dashauth.UserInfo{Subject: "u1", Roles: []string{"admin"}}})
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	actions := got.Slots["main"][0].Slots["rowActions"]
	if len(actions) != 1 {
		t.Errorf("admin should see admin-only action; got %d", len(actions))
	}
}

func TestBuildGraph_RouteNotFound(t *testing.T) {
	r := NewRegistry()
	build := NewGraphBuilder(r, NewWardenRegistry())
	_, err := build.Build(context.Background(), "users", "/nope", Principal{})
	if err == nil {
		t.Error("expected not-found error")
	}
}
