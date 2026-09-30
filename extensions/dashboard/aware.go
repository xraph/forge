package dashboard

import (
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
)

// DashboardAuthAware is an optional interface that Forge extensions can
// implement to provide authentication for the dashboard. The dashboard
// auto-discovers extensions implementing this interface during Start()
// and calls RegisterDashboardAuth with itself, allowing the extension
// to set up auth checking, required roles, and tenant resolution.
//
// Example implementation:
//
//	func (a *AuthExtension) RegisterDashboardAuth(ext *dashboard.Extension) {
//	    ext.SetAuthChecker(myAuthChecker)
//	    ext.EnableAuth()
//	}
//
// Wiring an auth extension into the contract:
//
// Auth extensions plug in by implementing both DashboardAuthAware *and*
// ContractContributorAware:
//
//   - DashboardAuthAware.RegisterDashboardAuth wires the AuthChecker so
//     /api/dashboard/v1/principal returns the current user. That endpoint
//     distinguishes the 401 envelope (auth required) from the 200
//     `{authenticated:false}` envelope (auth disabled), so a client can tell
//     "log in" apart from "auth is off".
//   - ContractContributorAware.RegisterContractContributor registers the
//     `auth.login` command intent (and optionally `auth.logout`) on the
//     dispatcher.
//
// This is the whole server side. The dashboard serves no login page of its
// own: the React dashboard reads /principal to decide whether to show a login
// gate, and the login UI itself belongs to the auth extension's plugin.
//
// Example combined integration sketch:
//
//	func (a *AuthsomeExtension) RegisterDashboardAuth(ext *dashboard.Extension) {
//	    ext.SetAuthChecker(a.checker)
//	    ext.EnableAuth()
//	}
//	func (a *AuthsomeExtension) RegisterContractContributor(
//	    disp *dispatcher.Dispatcher,
//	    reg contract.Registry,
//	    wreg contract.WardenRegistry,
//	) error {
//	    return authsomecontract.Register(disp, reg, wreg, authsomecontract.Deps{
//	        Sessions: a.sessions, // registers the `auth.login` command
//	    })
//	}
type DashboardAuthAware interface {
	RegisterDashboardAuth(ext *Extension)
}

// ContractContributorAware is an optional interface that Forge extensions can
// implement to register a contract-based dashboard contributor (the slice (f)+
// shape — declarative YAML manifest + typed dispatcher handlers). The
// dashboard auto-discovers extensions implementing this interface during
// Start() and calls RegisterContractContributor with the dashboard's contract
// registry, warden registry, and dispatcher.
//
// This is the only way an extension contributes to the dashboard. Its UI
// ships separately, as a React plugin that talks to these intents.
//
// Example implementation:
//
//	func (e *StreamingExtension) RegisterContractContributor(
//	    disp *dispatcher.Dispatcher,
//	    reg contract.Registry,
//	    wreg contract.WardenRegistry,
//	) error {
//	    return streamingcontract.Register(disp, reg, wreg, streamingcontract.Deps{
//	        Manager: func() streaming.Manager { return e.manager },
//	        Config:  func() streaming.Config { return e.config },
//	    })
//	}
type ContractContributorAware interface {
	RegisterContractContributor(
		disp *dispatcher.Dispatcher,
		reg contract.Registry,
		wreg contract.WardenRegistry,
	) error
}

// DashboardStatus is what an extension reports about itself to the dashboard.
//
// Set Configured explicitly on every value you return. The struct's zero value
// is Configured=false, the restrictive answer, so a bare DashboardStatus{}
// returned from a switch default asks the dashboard for a setup panel.
//
// The permissive default lives elsewhere: an extension that does not implement
// DashboardStatusAware at all is reported with an empty Version and
// Configured=true, so the plugin host skips the version check rather than
// failing it, and renders the plugin normally. Nobody has to implement this to
// keep working.
type DashboardStatus struct {
	// Version is the extension's semver, checked against the plugin's
	// `requires` range. Empty means "do not check".
	Version string `json:"version,omitempty"`
	// Configured reports whether the extension has everything it needs to
	// serve. False makes the dashboard render the plugin's setup guide.
	Configured bool `json:"configured"`
	// Message is optional detail shown in the setup panel.
	Message string `json:"message,omitempty"`
}

// DashboardStatusAware is an optional interface a Forge extension can implement
// to tell the dashboard its version and whether it is configured.
//
// Without it the dashboard would have to infer setup state by calling a data
// method and interpreting the failure, which cannot distinguish "not configured
// yet" from "configured but failing" from "you lack permission". Those are three
// different screens.
type DashboardStatusAware interface {
	DashboardStatus() DashboardStatus
}
