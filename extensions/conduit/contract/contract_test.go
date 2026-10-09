package contract

import (
	"errors"
	"testing"

	"github.com/xraph/forge/extensions/conduit/core"
	"github.com/xraph/forge/extensions/conduit/discovery"
	"github.com/xraph/forge/extensions/conduit/providers/memory"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
)

func testDeps(t *testing.T) Deps {
	t.Helper()

	r, err := core.New(core.Config{Identity: core.Identity{Namespace: "prod", ServiceID: "billing", InstanceID: "one"}}, core.WithProvider("events", memory.New()), core.WithRegistry(discovery.NewStatic()))
	if err != nil {
		t.Fatal(err)
	}

	return Deps{Runtime: func() *core.Runtime { return r }}
}
func TestManifestAndScope(t *testing.T) {
	deps := testDeps(t)
	if err := Register(dispatcher.New(nil), dash.NewRegistry(), dash.NewWardenRegistry(), deps); err != nil {
		t.Fatal(err)
	}

	for _, claims := range []map[string]any{{"namespace": ""}, {"namespace": 42}, {"namespace": "elsewhere"}, {"service_id": "other"}, {"service_id": []string{"billing"}}} {
		if _, err := overview(deps)(t.Context(), struct{}{}, dash.Principal{Claims: claims}); !errors.Is(err, dash.ErrPermissionDenied) {
			t.Fatalf("scope %v was not denied: %v", claims, err)
		}
	}

	if _, err := overview(deps)(t.Context(), struct{}{}, dash.Principal{}); err != nil {
		t.Fatal(err)
	}

	if _, err := deadletters(deps)(t.Context(), ListInput{}, dash.Principal{}); !errors.Is(err, dash.ErrBadRequest) {
		t.Fatal("missing provider not rejected")
	}
}
