package security

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/xraph/forge/errors"
)

func TestCookieManager_SetCookie(t *testing.T) {
	tests := []struct {
		name     string
		defaults CookieOptions
		opts     *CookieOptions
		expected http.Cookie
	}{
		{
			name:     "with defaults",
			defaults: DefaultCookieOptions(),
			opts:     nil,
			expected: http.Cookie{
				Name:     "test",
				Value:    "value",
				Path:     "/",
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteLaxMode,
			},
		},
		{
			name:     "with custom options",
			defaults: DefaultCookieOptions(),
			opts: &CookieOptions{
				Path:     "/api",
				Domain:   "example.com",
				MaxAge:   3600,
				Secure:   true,
				HttpOnly: true,
				SameSite: SameSiteStrict,
			},
			expected: http.Cookie{
				Name:     "test",
				Value:    "value",
				Path:     "/api",
				Domain:   "example.com",
				MaxAge:   3600,
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cm := NewCookieManager(tt.defaults)
			w := httptest.NewRecorder()

			cm.SetCookie(w, "test", "value", tt.opts)

			cookies := w.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatalf("expected 1 cookie, got %d", len(cookies))
			}

			cookie := cookies[0]
			if cookie.Name != tt.expected.Name {
				t.Errorf("expected name %s, got %s", tt.expected.Name, cookie.Name)
			}

			if cookie.Value != tt.expected.Value {
				t.Errorf("expected value %s, got %s", tt.expected.Value, cookie.Value)
			}

			if cookie.Path != tt.expected.Path {
				t.Errorf("expected path %s, got %s", tt.expected.Path, cookie.Path)
			}

			if cookie.Domain != tt.expected.Domain {
				t.Errorf("expected domain %s, got %s", tt.expected.Domain, cookie.Domain)
			}

			if cookie.MaxAge != tt.expected.MaxAge {
				t.Errorf("expected max age %d, got %d", tt.expected.MaxAge, cookie.MaxAge)
			}

			if cookie.Secure != tt.expected.Secure {
				t.Errorf("expected secure %v, got %v", tt.expected.Secure, cookie.Secure)
			}

			if cookie.HttpOnly != tt.expected.HttpOnly {
				t.Errorf("expected http only %v, got %v", tt.expected.HttpOnly, cookie.HttpOnly)
			}

			if cookie.SameSite != tt.expected.SameSite {
				t.Errorf("expected same site %v, got %v", tt.expected.SameSite, cookie.SameSite)
			}
		})
	}
}

func TestCookieManager_GetCookie(t *testing.T) {
	cm := NewCookieManager(DefaultCookieOptions())

	// Create request with cookie
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test",
		Value: "value",
	})

	value, err := cm.GetCookie(req, "test")
	if err != nil {
		t.Fatalf("failed to get cookie: %v", err)
	}

	if value != "value" {
		t.Errorf("expected value 'value', got %s", value)
	}
}

func TestCookieManager_GetCookie_NotFound(t *testing.T) {
	cm := NewCookieManager(DefaultCookieOptions())

	req := httptest.NewRequest(http.MethodGet, "/", nil)

	_, err := cm.GetCookie(req, "nonexistent")
	if !errors.Is(err, ErrCookieNotFound) {
		t.Errorf("expected ErrCookieNotFound, got %v", err)
	}
}

func TestCookieManager_DeleteCookie(t *testing.T) {
	cm := NewCookieManager(DefaultCookieOptions())
	w := httptest.NewRecorder()

	cm.DeleteCookie(w, "test", nil)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != "test" {
		t.Errorf("expected name 'test', got %s", cookie.Name)
	}

	if cookie.Value != "" {
		t.Errorf("expected empty value, got %s", cookie.Value)
	}

	if cookie.MaxAge != -1 {
		t.Errorf("expected max age -1, got %d", cookie.MaxAge)
	}
}

func TestCookieManager_HasCookie(t *testing.T) {
	cm := NewCookieManager(DefaultCookieOptions())

	// Request with cookie
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{
		Name:  "test",
		Value: "value",
	})

	if !cm.HasCookie(req, "test") {
		t.Error("expected HasCookie to return true")
	}

	if cm.HasCookie(req, "nonexistent") {
		t.Error("expected HasCookie to return false for nonexistent cookie")
	}
}

func TestCookieManager_GetAllCookies(t *testing.T) {
	cm := NewCookieManager(DefaultCookieOptions())

	// Request with multiple cookies
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.AddCookie(&http.Cookie{Name: "cookie1", Value: "value1"})
	req.AddCookie(&http.Cookie{Name: "cookie2", Value: "value2"})
	req.AddCookie(&http.Cookie{Name: "cookie3", Value: "value3"})

	cookies := cm.GetAllCookies(req)
	if len(cookies) != 3 {
		t.Errorf("expected 3 cookies, got %d", len(cookies))
	}
}

func TestSameSiteConversion(t *testing.T) {
	tests := []struct {
		input    SameSiteMode
		expected http.SameSite
	}{
		{SameSiteDefault, http.SameSiteDefaultMode},
		{SameSiteLax, http.SameSiteLaxMode},
		{SameSiteStrict, http.SameSiteStrictMode},
		{SameSiteNone, http.SameSiteNoneMode},
	}

	cm := NewCookieManager(DefaultCookieOptions())

	for _, tt := range tests {
		t.Run(string(tt.input), func(t *testing.T) {
			result := cm.convertSameSite(tt.input)
			if result != tt.expected {
				t.Errorf("expected %v, got %v", tt.expected, result)
			}
		})
	}
}

func TestCookieOptions_WithExpires(t *testing.T) {
	cm := NewCookieManager(DefaultCookieOptions())
	w := httptest.NewRecorder()

	expires := time.Now().Add(1 * time.Hour).UTC()
	opts := &CookieOptions{
		Expires: expires,
	}

	cm.SetCookie(w, "test", "value", opts)

	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	// Compare in UTC and allow for small time differences (within 1 second)
	diff := cookie.Expires.Sub(expires)
	if diff < 0 {
		diff = -diff
	}

	if diff > time.Second {
		t.Errorf("expected expires %v, got %v (diff: %v)", expires, cookie.Expires, diff)
	}
}
