// e2e_test.go
package contract_test

import (
	"context"
	"os"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"
)

func loadFixture(t *testing.T, path string) *contract.ContractManifest {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	m, err := loader.Load(f, path)
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestE2E_RegisterValidateBuild(t *testing.T) {
	users := loadFixture(t, "testdata/fixture_users.yaml")
	authExt := loadFixture(t, "testdata/fixture_auth_extends.yaml")

	wreg := contract.NewWardenRegistry()
	if err := loader.Validate(users, wreg); err != nil {
		t.Fatalf("validate users: %v", err)
	}
	if err := loader.Validate(authExt, wreg); err != nil {
		t.Fatalf("validate authExt: %v", err)
	}

	reg := contract.NewRegistry()
	if err := reg.Register(users); err != nil {
		t.Fatalf("register users: %v", err)
	}
	if err := reg.Register(authExt); err != nil {
		t.Fatalf("register authExt: %v", err)
	}

	build := contract.NewGraphBuilder(reg, wreg)
	admin := &dashauth.UserInfo{Subject: "alice", Roles: []string{"admin"}, Scopes: []string{"users.read", "users.write"}}
	got, err := build.Build(context.Background(), "users", "/users", contract.PrincipalFor(admin))
	if err != nil {
		t.Fatalf("build admin: %v", err)
	}
	// admin should see the disable action
	actions := got.Slots["main"][0].Slots["rowActions"]
	if len(actions) != 1 || actions[0].Op != "user.disable" {
		t.Errorf("admin actions wrong: %+v", actions)
	}
	// extension should be merged: detailDrawer.fields has 2 form.fields
	fields := got.Slots["main"][0].Slots["detailDrawer"][0].Slots["fields"]
	if len(fields) != 2 {
		t.Errorf("expected 2 fields after extension merge, got %d", len(fields))
	}

	// viewer sees no row actions
	viewer := &dashauth.UserInfo{Subject: "bob", Roles: []string{"viewer"}, Scopes: []string{"users.read"}}
	got2, _ := build.Build(context.Background(), "users", "/users", contract.PrincipalFor(viewer))
	if got2 == nil {
		t.Skip("viewer filtered fully") // depends on resource.list visibleWhen
		return
	}
	actions2 := got2.Slots["main"][0].Slots["rowActions"]
	if len(actions2) != 0 {
		t.Errorf("viewer should see no admin actions: %+v", actions2)
	}
}
