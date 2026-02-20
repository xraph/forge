package dashauth

import (
	g "maragu.dev/gomponents"

	"github.com/xraph/forgeui/router"
)

// AuthPageType identifies the kind of authentication page.
type AuthPageType string

const (
	PageLogin          AuthPageType = "login"
	PageLogout         AuthPageType = "logout"
	PageRegister       AuthPageType = "register"
	PageForgotPassword AuthPageType = "forgot-password"
	PageResetPassword  AuthPageType = "reset-password"
	PageCallback       AuthPageType = "callback"
	PageProfile        AuthPageType = "profile"
)

// AuthPageDescriptor describes an auth page contributed to the dashboard.
type AuthPageDescriptor struct {
	// Type identifies the kind of auth page (login, logout, register, etc.).
	Type AuthPageType

	// Path is the route path relative to the auth base path (e.g. "/login").
	Path string

	// Title is the page title shown in the browser tab.
	Title string

	// Icon is the optional icon name for the page.
	Icon string
}

// AuthPageProvider contributes authentication pages to the dashboard.
// Auth extensions implement this interface to provide login, register,
// and other auth-related pages that integrate into the dashboard shell.
type AuthPageProvider interface {
	// AuthPages returns the list of auth pages this provider contributes.
	AuthPages() []AuthPageDescriptor

	// RenderAuthPage renders the HTML for an auth page (GET request).
	// The page type identifies which auth page to render.
	RenderAuthPage(ctx *router.PageContext, pageType AuthPageType) (g.Node, error)

	// HandleAuthAction handles a form submission for an auth page (POST request).
	// Returns:
	//   - redirectURL: if non-empty, the user should be redirected to this URL
	//   - errNode: if non-nil, re-render the page with this error content
	//   - err: infrastructure errors
	HandleAuthAction(ctx *router.PageContext, pageType AuthPageType) (redirectURL string, errNode g.Node, err error)
}
