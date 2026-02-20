package contributor

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestEmbeddedContributor_Manifest(t *testing.T) {
	m := &Manifest{
		Name:        "test",
		DisplayName: "Test Contributor",
		Version:     "1.0.0",
	}

	ec := NewEmbeddedContributor(m, fstest.MapFS{})
	if ec.Manifest() != m {
		t.Error("Manifest() should return the same manifest")
	}
}

func TestEmbeddedContributor_RenderPage(t *testing.T) {
	testFS := fstest.MapFS{
		"pages/index.html": &fstest.MapFile{
			Data: []byte(`<!DOCTYPE html><html><head><title>Test</title></head><body><div>Overview</div></body></html>`),
		},
		"pages/sessions/index.html": &fstest.MapFile{
			Data: []byte(`<div>Sessions Page</div>`),
		},
		"pages/users.html": &fstest.MapFile{
			Data: []byte(`<p>Users</p>`),
		},
	}

	m := &Manifest{Name: "test"}
	ec := NewEmbeddedContributor(m, testFS)
	ctx := context.Background()

	tests := []struct {
		name     string
		route    string
		expected string
		wantErr  bool
	}{
		{
			name:     "root route extracts body",
			route:    "/",
			expected: "<div>Overview</div>",
		},
		{
			name:     "subdirectory route",
			route:    "/sessions",
			expected: "<div>Sessions Page</div>",
		},
		{
			name:     "file-based route",
			route:    "/users",
			expected: "<p>Users</p>",
		},
		{
			name:    "nonexistent page",
			route:   "/nonexistent",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			node, err := ec.RenderPage(ctx, tt.route, Params{})
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}

				if !errors.Is(err, ErrPageNotFound) {
					t.Errorf("expected ErrPageNotFound, got %v", err)
				}

				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if node == nil {
				t.Fatal("expected node, got nil")
			}
		})
	}
}

func TestEmbeddedContributor_RenderWidget(t *testing.T) {
	testFS := fstest.MapFS{
		"widgets/active-sessions.html": &fstest.MapFile{
			Data: []byte(`<span class="count">42</span>`),
		},
	}

	m := &Manifest{Name: "test"}
	ec := NewEmbeddedContributor(m, testFS)
	ctx := context.Background()

	t.Run("existing widget", func(t *testing.T) {
		node, err := ec.RenderWidget(ctx, "active-sessions")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if node == nil {
			t.Fatal("expected node, got nil")
		}
	})

	t.Run("nonexistent widget", func(t *testing.T) {
		_, err := ec.RenderWidget(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error, got nil")
		}

		if !errors.Is(err, ErrWidgetNotFound) {
			t.Errorf("expected ErrWidgetNotFound, got %v", err)
		}
	})
}

func TestEmbeddedContributor_RenderSettings(t *testing.T) {
	testFS := fstest.MapFS{
		"settings/auth-config.html": &fstest.MapFile{
			Data: []byte(`<form><input name="ttl" value="3600"></form>`),
		},
	}

	m := &Manifest{Name: "test"}
	ec := NewEmbeddedContributor(m, testFS)
	ctx := context.Background()

	t.Run("existing setting", func(t *testing.T) {
		node, err := ec.RenderSettings(ctx, "auth-config")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if node == nil {
			t.Fatal("expected node, got nil")
		}
	})

	t.Run("nonexistent setting", func(t *testing.T) {
		_, err := ec.RenderSettings(ctx, "nonexistent")
		if err == nil {
			t.Error("expected error, got nil")
		}

		if !errors.Is(err, ErrSettingNotFound) {
			t.Errorf("expected ErrSettingNotFound, got %v", err)
		}
	})
}

func TestEmbeddedContributor_AssetsHandler(t *testing.T) {
	testFS := fstest.MapFS{
		"assets/style.css": &fstest.MapFile{
			Data: []byte(`body { color: red; }`),
		},
		"assets/app.js": &fstest.MapFile{
			Data: []byte(`console.log("hello")`),
		},
	}

	m := &Manifest{Name: "test"}
	ec := NewEmbeddedContributor(m, testFS)
	handler := ec.AssetsHandler()

	t.Run("existing asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/style.css", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}

		body, _ := io.ReadAll(w.Body)
		if string(body) != `body { color: red; }` {
			t.Errorf("unexpected body: %s", string(body))
		}
	})

	t.Run("nonexistent asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/nonexistent.css", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("expected 404, got %d", w.Code)
		}
	})
}

func TestEmbeddedContributor_CustomDirs(t *testing.T) {
	testFS := fstest.MapFS{
		"custom-pages/index.html": &fstest.MapFile{
			Data: []byte(`<div>Custom</div>`),
		},
		"custom-widgets/test.html": &fstest.MapFile{
			Data: []byte(`<span>Widget</span>`),
		},
		"custom-settings/cfg.html": &fstest.MapFile{
			Data: []byte(`<form>Settings</form>`),
		},
		"static/app.css": &fstest.MapFile{
			Data: []byte(`body{}`),
		},
	}

	m := &Manifest{Name: "test"}
	ec := NewEmbeddedContributor(m, testFS,
		WithPagesDir("custom-pages"),
		WithWidgetsDir("custom-widgets"),
		WithSettingsDir("custom-settings"),
		WithAssetsDir("static"),
	)
	ctx := context.Background()

	node, err := ec.RenderPage(ctx, "/", Params{})
	if err != nil {
		t.Fatalf("RenderPage: %v", err)
	}

	if node == nil {
		t.Fatal("expected node")
	}

	node, err = ec.RenderWidget(ctx, "test")
	if err != nil {
		t.Fatalf("RenderWidget: %v", err)
	}

	if node == nil {
		t.Fatal("expected node")
	}

	node, err = ec.RenderSettings(ctx, "cfg")
	if err != nil {
		t.Fatalf("RenderSettings: %v", err)
	}

	if node == nil {
		t.Fatal("expected node")
	}
}

func TestEmbeddedContributor_AssetsHandler_NoAssetsDir(t *testing.T) {
	testFS := fstest.MapFS{
		"pages/index.html": &fstest.MapFile{Data: []byte(`<div>Test</div>`)},
	}

	m := &Manifest{Name: "test"}
	ec := NewEmbeddedContributor(m, testFS)
	handler := ec.AssetsHandler()

	req := httptest.NewRequest(http.MethodGet, "/anything.css", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 when assets dir doesn't exist, got %d", w.Code)
	}
}
