package streaming

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard"
)

// capabilitiesResponse is the shape the dashboard's discovery endpoint serves.
// Only the fields this test asserts on are modelled.
type capabilitiesResponse struct {
	Contributors []struct {
		Name    string `json:"name"`
		Intents []struct {
			Name string `json:"name"`
		} `json:"intents"`
	} `json:"contributors"`
}

// TestDashboardDiscovery_PublishesStreamingContract boots a real Forge app with
// the real dashboard and streaming extensions, lets the dashboard's own
// auto-discovery loop register streaming's contract contributor, and asks the
// real capabilities endpoint what it ended up with.
//
// This covers a gap that unit-level manifest tests cannot: loader.Load and
// loader.Validate both accept a manifest that contract.Registry.Register then
// rejects, and the discovery loop logs that rejection and carries on. The
// visible symptom is not an error anywhere. It is streaming-contract quietly
// missing from /capabilities, with none of its intents reachable through the
// dispatcher. Asserting on the served capabilities is the only assertion that
// catches it.
func TestDashboardDiscovery_PublishesStreamingContract(t *testing.T) {
	app := forge.New(
		forge.WithAppName("streaming-contract-discovery-test"),
		forge.WithAppVersion("1.0.0"),
		forge.WithAppLogger(forge.NewNoopLogger()),
		forge.WithConfig(forge.DefaultAppConfig()),
		forge.WithExtensions(NewExtension(), dashboard.NewExtension()),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start app: %v", err)
	}

	t.Cleanup(func() {
		if err := app.Stop(context.Background()); err != nil {
			t.Errorf("stop app: %v", err)
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/dashboard/api/dashboard/v1/capabilities", nil)
	w := httptest.NewRecorder()
	app.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("capabilities status = %d, body = %s", w.Code, w.Body)
	}

	var resp capabilitiesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal capabilities %s: %v", w.Body.Bytes(), err)
	}

	var streamingIntents []string

	names := make([]string, 0, len(resp.Contributors))

	for _, c := range resp.Contributors {
		names = append(names, c.Name)

		if c.Name != "streaming-contract" {
			continue
		}

		for _, in := range c.Intents {
			streamingIntents = append(streamingIntents, in.Name)
		}
	}

	if streamingIntents == nil {
		t.Fatalf("streaming-contract absent from capabilities; contributors = %v.\n"+
			"The dashboard swallows contributor registration errors, so this is what a "+
			"rejected manifest looks like from the outside. Check the dashboard's startup "+
			"log for \"failed to register contract contributor\".", names)
	}

	// Every intent the manifest declares has to survive registration, not just
	// the contributor name. A partial manifest would still produce a
	// contributor entry here.
	want := []string{
		"stats", "connections.list", "rooms.list", "rooms.detail", "rooms.members",
		"rooms.moderation", "channels.list", "presence.list", "config",
		"rooms.create", "rooms.delete", "rooms.send-message", "presence.set",
		"connections.kick",
	}

	got := make(map[string]bool, len(streamingIntents))
	for _, in := range streamingIntents {
		got[in] = true
	}

	for _, w := range want {
		if !got[w] {
			t.Errorf("intent %q missing from streaming-contract capabilities; got %v", w, streamingIntents)
		}
	}
}
