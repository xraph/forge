package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCtx_SetCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Set a cookie
	ctx.SetCookie("session_id", "abc123", 3600)

	// Check response headers
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != "session_id" {
		t.Errorf("expected cookie name 'session_id', got '%s'", cookie.Name)
	}

	if cookie.Value != "abc123" {
		t.Errorf("expected cookie value 'abc123', got '%s'", cookie.Value)
	}

	if cookie.MaxAge != 3600 {
		t.Errorf("expected MaxAge 3600, got %d", cookie.MaxAge)
	}

	if !cookie.HttpOnly {
		t.Error("expected HttpOnly to be true")
	}

	if !cookie.Secure {
		t.Error("expected Secure to be true")
	}

	if cookie.Path != "/" {
		t.Errorf("expected Path '/', got '%s'", cookie.Path)
	}

	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("expected SameSite Lax, got %v", cookie.SameSite)
	}
}

func TestCtx_SetCookieWithOptions(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Set a cookie with custom options
	ctx.SetCookieWithOptions("auth_token", "xyz789", "/api", "example.com", 7200, false, false)

	// Check response headers
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != "auth_token" {
		t.Errorf("expected cookie name 'auth_token', got '%s'", cookie.Name)
	}

	if cookie.Value != "xyz789" {
		t.Errorf("expected cookie value 'xyz789', got '%s'", cookie.Value)
	}

	if cookie.Path != "/api" {
		t.Errorf("expected Path '/api', got '%s'", cookie.Path)
	}

	if cookie.Domain != "example.com" {
		t.Errorf("expected Domain 'example.com', got '%s'", cookie.Domain)
	}

	if cookie.MaxAge != 7200 {
		t.Errorf("expected MaxAge 7200, got %d", cookie.MaxAge)
	}

	if cookie.HttpOnly {
		t.Error("expected HttpOnly to be false")
	}

	if cookie.Secure {
		t.Error("expected Secure to be false")
	}
}

func TestCtx_Cookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	// Add cookies to request
	req.AddCookie(&http.Cookie{Name: "user_id", Value: "123"})
	req.AddCookie(&http.Cookie{Name: "theme", Value: "dark"})

	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Test getting existing cookie
	value, err := ctx.Cookie("user_id")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if value != "123" {
		t.Errorf("expected value '123', got '%s'", value)
	}

	// Test getting another cookie
	value, err = ctx.Cookie("theme")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}

	if value != "dark" {
		t.Errorf("expected value 'dark', got '%s'", value)
	}

	// Test getting non-existent cookie
	_, err = ctx.Cookie("missing")
	if err == nil {
		t.Error("expected error for missing cookie, got nil")
	}
}

func TestCtx_HasCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "session", Value: "abc"})

	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Test existing cookie
	if !ctx.HasCookie("session") {
		t.Error("expected HasCookie to return true for existing cookie")
	}

	// Test non-existent cookie
	if ctx.HasCookie("missing") {
		t.Error("expected HasCookie to return false for missing cookie")
	}
}

func TestCtx_DeleteCookie(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Delete a cookie
	ctx.DeleteCookie("session_id")

	// Check response headers
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected 1 cookie, got %d", len(cookies))
	}

	cookie := cookies[0]
	if cookie.Name != "session_id" {
		t.Errorf("expected cookie name 'session_id', got '%s'", cookie.Name)
	}

	if cookie.Value != "" {
		t.Errorf("expected empty cookie value, got '%s'", cookie.Value)
	}

	if cookie.MaxAge != -1 {
		t.Errorf("expected MaxAge -1, got %d", cookie.MaxAge)
	}
}

func TestCtx_GetAllCookies(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "cookie1", Value: "value1"})
	req.AddCookie(&http.Cookie{Name: "cookie2", Value: "value2"})
	req.AddCookie(&http.Cookie{Name: "cookie3", Value: "value3"})

	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Get all cookies
	cookies := ctx.GetAllCookies()

	// Check count
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies, got %d", len(cookies))
	}

	// Check values
	expected := map[string]string{
		"cookie1": "value1",
		"cookie2": "value2",
		"cookie3": "value3",
	}

	for name, expectedValue := range expected {
		if value, ok := cookies[name]; !ok {
			t.Errorf("cookie %s not found", name)
		} else if value != expectedValue {
			t.Errorf("expected cookie %s to have value '%s', got '%s'", name, expectedValue, value)
		}
	}
}

func TestCtx_MultipleCookieOperations(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.AddCookie(&http.Cookie{Name: "existing", Value: "old_value"})

	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Read existing cookie
	value, err := ctx.Cookie("existing")
	if err != nil {
		t.Errorf("unexpected error reading cookie: %v", err)
	}

	if value != "old_value" {
		t.Errorf("expected 'old_value', got '%s'", value)
	}

	// Set new cookies
	ctx.SetCookie("new_cookie1", "value1", 3600)
	ctx.SetCookie("new_cookie2", "value2", 7200)

	// Delete a cookie
	ctx.DeleteCookie("existing")

	// Check response contains all operations
	cookies := w.Result().Cookies()
	if len(cookies) != 3 {
		t.Fatalf("expected 3 cookies in response, got %d", len(cookies))
	}

	// Verify each cookie
	cookieMap := make(map[string]*http.Cookie)
	for _, c := range cookies {
		cookieMap[c.Name] = c
	}

	if c, ok := cookieMap["new_cookie1"]; !ok {
		t.Error("new_cookie1 not found")
	} else if c.Value != "value1" {
		t.Errorf("new_cookie1 value = '%s', want 'value1'", c.Value)
	}

	if c, ok := cookieMap["new_cookie2"]; !ok {
		t.Error("new_cookie2 not found")
	} else if c.Value != "value2" {
		t.Errorf("new_cookie2 value = '%s', want 'value2'", c.Value)
	}

	if c, ok := cookieMap["existing"]; !ok {
		t.Error("existing cookie delete not found")
	} else if c.MaxAge != -1 {
		t.Errorf("existing cookie MaxAge = %d, want -1", c.MaxAge)
	}
}
