package dashboard

import "errors"

var (
	// ErrPageNotFound is returned when a requested page does not exist.
	ErrPageNotFound = errors.New("dashboard: page not found")

	// ErrWidgetNotFound is returned when a requested widget does not exist.
	ErrWidgetNotFound = errors.New("dashboard: widget not found")

	// ErrSettingNotFound is returned when a requested setting does not exist.
	ErrSettingNotFound = errors.New("dashboard: setting not found")

	// ErrContributorExists is returned when a contributor with the same name is already registered.
	ErrContributorExists = errors.New("dashboard: contributor already registered")

	// ErrContributorNotFound is returned when a contributor is not found in the registry.
	ErrContributorNotFound = errors.New("dashboard: contributor not found")

	// ErrRemoteUnreachable is returned when a remote contributor cannot be reached.
	ErrRemoteUnreachable = errors.New("dashboard: remote contributor unreachable")

	// ErrManifestFetch is returned when fetching a remote manifest fails.
	ErrManifestFetch = errors.New("dashboard: failed to fetch remote manifest")

	// ErrDiscoveryTimeout is returned when service discovery times out.
	ErrDiscoveryTimeout = errors.New("dashboard: service discovery timed out")

	// ErrRecoveryFailed is returned when UI recovery fails.
	ErrRecoveryFailed = errors.New("dashboard: UI recovery failed")

	// ErrCollectorNotInitialized is returned when the data collector is not initialized.
	ErrCollectorNotInitialized = errors.New("dashboard: collector not initialized")
)
