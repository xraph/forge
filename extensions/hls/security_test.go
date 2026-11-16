package hls

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/hls/storage"
	"github.com/xraph/trove"
	"github.com/xraph/trove/drivers/memdriver"
)

type keyRecorder struct {
	mockStorage

	mu   sync.Mutex
	keys []string
}

func (k *keyRecorder) Put(_ context.Context, key string, _ io.Reader, _ string) error {
	k.mu.Lock()
	defer k.mu.Unlock()

	k.keys = append(k.keys, key)

	return nil
}

func liveStreams(t *testing.T, cfg Config) (*Manager, *keyRecorder, *Stream, *Stream) {
	t.Helper()

	rec := &keyRecorder{}

	cfg.EnableAutoCleanup = false

	m, err := NewManager(cfg, storage.NewHLSStorage(rec, "hls"))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(m.Stop)

	ctx := context.Background()
	profiles := []TranscodeProfile{{Name: "p"}}

	a, err := m.CreateStream(ctx, StreamOptions{Type: StreamTypeLive, Title: "attacker", TranscodeProfiles: profiles})
	if err != nil {
		t.Fatal(err)
	}

	b, err := m.CreateStream(ctx, StreamOptions{Type: StreamTypeLive, Title: "victim", TranscodeProfiles: profiles})
	if err != nil {
		t.Fatal(err)
	}

	if err := m.StartLiveStream(ctx, a.ID); err != nil {
		t.Fatal(err)
	}

	rec.keys = nil

	return m, rec, a, b
}

// Regression: the VariantID from the ingest body was joined into the storage
// key unchecked, so "../<B>/<variant>" wrote into stream B's prefix.
func TestIngestSegment_RefusesForeignOrTraversingVariant(t *testing.T) {
	m, rec, a, b := liveStreams(t, DefaultConfig())

	for _, variant := range []string{
		"../" + b.ID + "/" + b.Variants[0].ID,
		b.Variants[0].ID, // a real variant, but of another stream
		"..",
		"",
		a.Variants[0].ID + "/x",
	} {
		err := m.IngestSegment(context.Background(), a.ID, &Segment{VariantID: variant, SequenceNum: 5, Data: []byte("evil")})
		if !errors.Is(err, ErrUnknownVariant) {
			t.Errorf("variant %q: err = %v, want ErrUnknownVariant", variant, err)
		}
	}

	if len(rec.keys) != 0 {
		t.Fatalf("refused ingests still wrote: %v", rec.keys)
	}

	if err := m.IngestSegment(context.Background(), a.ID, &Segment{VariantID: a.Variants[0].ID, SequenceNum: 0, Data: []byte("ok")}); err != nil {
		t.Fatalf("own variant refused: %v", err)
	}

	want := "hls/" + a.ID + "/" + a.Variants[0].ID + "/segment_0.ts"
	if len(rec.keys) == 0 || rec.keys[0] != want {
		t.Fatalf("keys = %v, want first %s", rec.keys, want)
	}
}

// Regression: MaxSegmentSize was stored but never enforced.
func TestIngestSegment_EnforcesMaxSegmentSize(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MaxSegmentSize = 8

	m, rec, a, _ := liveStreams(t, cfg)

	err := m.IngestSegment(context.Background(), a.ID, &Segment{VariantID: a.Variants[0].ID, Data: make([]byte, 9)})
	if !errors.Is(err, ErrSegmentTooLarge) {
		t.Fatalf("err = %v, want ErrSegmentTooLarge", err)
	}

	if len(rec.keys) != 0 {
		t.Fatalf("oversized segment written: %v", rec.keys)
	}

	if err := m.IngestSegment(context.Background(), a.ID, &Segment{VariantID: a.Variants[0].ID, Data: make([]byte, 8)}); err != nil {
		t.Fatalf("segment at the limit refused: %v", err)
	}
}

// The storage layer refuses IDs that would leave their key segment even if a
// caller skips the manager.
func TestHLSStorage_RefusesTraversingIDs(t *testing.T) {
	rec := &keyRecorder{}
	s := storage.NewHLSStorage(rec, "hls")

	for _, id := range []string{"..", "../b", "a/b", `a\b`, ""} {
		if err := s.SaveSegment(context.Background(), "a", id, 0, nil); !errors.Is(err, storage.ErrInvalidKeyPart) {
			t.Errorf("SaveSegment variant %q: err = %v", id, err)
		}

		if err := s.SavePlaylist(context.Background(), id, "v", ""); !errors.Is(err, storage.ErrInvalidKeyPart) {
			t.Errorf("SavePlaylist stream %q: err = %v", id, err)
		}
	}

	if len(rec.keys) != 0 {
		t.Fatalf("refused writes reached the backend: %v", rec.keys)
	}
}

// startHLS runs the extension on an in-memory Trove. A global middleware
// signs the caller in as whoever X-User names, standing in for the auth
// extension.
func startHLS(t *testing.T, opts ...ConfigOption) forge.App {
	t.Helper()

	off := false
	app := forge.New(forge.WithAppName("hls-sec"), forge.WithAppTracingConfig(forge.TracingConfig{Enabled: &off}))

	drv := memdriver.New()
	if err := drv.Open(context.Background(), ""); err != nil {
		t.Fatal(err)
	}

	tr, err := trove.Open(drv)
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = tr.Close(context.Background()) })

	if err := forge.ProvideValue(app.Container(), tr); err != nil {
		t.Fatal(err)
	}

	app.Router().UseGlobal(func(next forge.Handler) forge.Handler {
		return func(ctx forge.Context) error {
			if u := ctx.Request().Header.Get("X-User"); u != "" {
				ctx.Set("user_id", u)
			}

			return next(ctx)
		}
	})

	opts = append([]ConfigOption{WithTranscoding(false)}, opts...)

	ext := NewExtension(opts...).(*Extension)
	ext.config.EnableAutoCleanup = false

	if err := ext.Register(app); err != nil {
		t.Fatal(err)
	}

	if err := ext.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = ext.Stop(context.Background()) })

	return app
}

func call(t *testing.T, app forge.App, method, path, user, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}

	if user != "" {
		req.Header.Set("X-User", user)
	}

	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, req)

	return rec
}

// Regression: WithAuth(true) was stored and never read, so the management
// API answered anyone.
func TestManagementAPI_RequireAuth(t *testing.T) {
	app := startHLS(t, WithAuth(true))

	create := `{"Title":"t","Type":"live","TranscodeProfiles":[{"Name":"p","Width":640,"Height":360,"Bitrate":800000}]}`

	for _, r := range []struct{ method, path, body string }{
		{http.MethodPost, "/hls/streams", create},
		{http.MethodGet, "/hls/streams", ""},
		{http.MethodGet, "/hls/streams/x", ""},
		{http.MethodDelete, "/hls/streams/x", ""},
		{http.MethodPost, "/hls/streams/x/start", ""},
		{http.MethodPost, "/hls/streams/x/stop", ""},
		{http.MethodPost, "/hls/streams/x/ingest", `{}`},
		{http.MethodGet, "/hls/streams/x/stats", ""},
	} {
		if rec := call(t, app, r.method, r.path, "", r.body); rec.Code != http.StatusUnauthorized {
			t.Errorf("anonymous %s %s = %d, want 401", r.method, r.path, rec.Code)
		}
	}

	rec := call(t, app, http.MethodPost, "/hls/streams", "alice", create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("signed-in create = %d %s", rec.Code, rec.Body)
	}

	// Stream has no json tags; the wire keys are its Go field names.
	var s struct {
		ID       string `json:"ID"`
		Variants []struct {
			ID string `json:"ID"`
		} `json:"Variants"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &s); err != nil {
		t.Fatal(err)
	}

	if rec := call(t, app, http.MethodPost, "/hls/streams/"+s.ID+"/start", "alice", ""); rec.Code != http.StatusOK {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}

	ingest := func(variant string) int {
		body := `{"VariantID":"` + variant + `","SequenceNum":0,"Duration":6,"Data":"` + base64.StdEncoding.EncodeToString([]byte("ts")) + `"}`

		return call(t, app, http.MethodPost, "/hls/streams/"+s.ID+"/ingest", "alice", body).Code
	}

	if code := ingest("../other/variant"); code != http.StatusBadRequest {
		t.Errorf("traversing variant over HTTP = %d, want 400", code)
	}

	if code := ingest(s.Variants[0].ID); code != http.StatusOK {
		t.Errorf("valid ingest over HTTP = %d, want 200", code)
	}
}

// The management API is not mounted unless asked for.
func TestManagementAPI_OffByDefault(t *testing.T) {
	app := startHLS(t)

	if rec := call(t, app, http.MethodGet, "/hls/streams", "", ""); rec.Code != http.StatusNotFound {
		t.Errorf("GET /hls/streams = %d with the API off, want 404", rec.Code)
	}

	open := startHLS(t, WithManagementAPI(true))
	if rec := call(t, open, http.MethodGet, "/hls/streams", "", ""); rec.Code != http.StatusOK {
		t.Errorf("GET /hls/streams = %d with the API on and auth off, want 200", rec.Code)
	}
}
