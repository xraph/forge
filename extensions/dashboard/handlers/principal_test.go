package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
)

func TestHandleAPIPrincipal_OK(t *testing.T) {
	user := &dashauth.UserInfo{
		Subject:     "alice",
		DisplayName: "Alice",
		Roles:       []string{"admin"},
		Scopes:      []string{"users.read"},
	}
	ctx := dashauth.WithUser(context.Background(), user)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/principal", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	HandleAPIPrincipalHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if body["subject"] != "alice" || body["displayName"] != "Alice" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestHandleAPIPrincipal_DisplayNameFallsBackToSubject(t *testing.T) {
	user := &dashauth.UserInfo{Subject: "u_42"}
	ctx := dashauth.WithUser(context.Background(), user)
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/principal", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	HandleAPIPrincipalHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var body map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &body)
	if body["displayName"] != "u_42" {
		t.Errorf("expected displayName fallback to subject, got %q", body["displayName"])
	}
}

func TestHandleAPIPrincipal_Unauthenticated(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/dashboard/v1/principal", nil)
	w := httptest.NewRecorder()
	HandleAPIPrincipalHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
