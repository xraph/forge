package workbench

import (
	"crypto/sha256"
	"crypto/subtle"
	"net/http"
	"strings"
)

func sameSecret(a, b string) bool {
	left, right := sha256.Sum256([]byte(a)), sha256.Sum256([]byte(b))

	return subtle.ConstantTimeCompare(left[:], right[:]) == 1
}
func (s *Server) authenticated(r *http.Request) bool {
	cookie, err := r.Cookie(s.cookieName)

	return err == nil && sameSecret(cookie.Value, s.session)
}
func (s *Server) handle(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
	w.Header().Set("X-Frame-Options", "DENY")

	if r.Host != s.host || (r.Header.Get("Origin") != "" && r.Header.Get("Origin") != s.origin) {
		apiFailure(w, http.StatusForbidden, "workbench origin rejected", nil)

		return
	}

	if r.URL.Path == "/" && r.URL.Query().Has("t") {
		if r.Method != http.MethodGet {
			apiFailure(w, 405, "method rejected", nil)

			return
		}

		s.authMu.Lock()

		valid := !s.exchanged && sameSecret(r.URL.Query().Get("t"), s.token)
		if valid {
			s.exchanged = true
		}
		s.authMu.Unlock()

		if !valid {
			apiFailure(w, 401, "sign-in token was invalid or already exchanged", nil)

			return
		}
		// #nosec G124 -- This listener only serves loopback HTTP; host and origin are pinned.
		http.SetCookie(w, &http.Cookie{Name: s.cookieName, Value: s.session, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode})
		http.Redirect(w, r, "/", http.StatusSeeOther)

		return
	}

	if !s.authenticated(r) {
		apiFailure(w, 401, "open the workbench URL printed by forge deploy start", nil)

		return
	}

	if strings.HasPrefix(r.URL.Path, "/api/") {
		if r.Header.Get("X-Forge-Workbench") != "1" || (r.Method != http.MethodGet && r.Header.Get("Origin") != s.origin) {
			apiFailure(w, 403, "workbench request header or origin missing", nil)

			return
		}

		s.api(w, r)

		return
	}

	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		apiFailure(w, 405, "method rejected", nil)

		return
	}

	if s.assets == nil {
		http.Error(w, "The deployment API is running. Embedded page assets are not included in this build.", http.StatusServiceUnavailable)

		return
	}

	s.assets.ServeHTTP(w, r)
}
