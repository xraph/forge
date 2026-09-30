package pilot

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

type stubExtensions []ExtensionInfo

func (s stubExtensions) ListExtensions() []ExtensionInfo { return s }

func TestExtensionsListHandler_ReturnsRegisteredExtensions(t *testing.T) {
	p := stubExtensions{
		{Name: "auth", DisplayName: "Authentication", Version: "1.0"},
		{Name: "cron", Version: "0.9"},
	}

	h := extensionsListHandler(p)
	res, err := h(context.Background(), struct{}{}, contract.Principal{})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if len(res.Extensions) != 2 {
		t.Fatalf("got %d, want 2", len(res.Extensions))
	}
	for _, e := range res.Extensions {
		if e.Name == "cron" && e.DisplayName != "cron" {
			t.Errorf("cron display name fallback = %q", e.DisplayName)
		}
		if e.Name == "auth" && e.DisplayName != "Authentication" {
			t.Errorf("auth display name = %q, want it kept", e.DisplayName)
		}
	}

	// Verify the result encodes cleanly to JSON.
	if _, err := json.Marshal(res); err != nil {
		t.Errorf("marshal: %v", err)
	}
}
