package contributor

import "errors"

var (
	// ErrContributorExists is returned when a contributor with the same name is already registered.
	ErrContributorExists = errors.New("dashboard: contributor already registered")

	// ErrContributorNotFound is returned when a contributor is not found in the registry.
	ErrContributorNotFound = errors.New("dashboard: contributor not found")

	// ErrPageNotFound is returned when a requested page does not exist in a contributor.
	ErrPageNotFound = errors.New("dashboard: page not found")

	// ErrWidgetNotFound is returned when a requested widget does not exist in a contributor.
	ErrWidgetNotFound = errors.New("dashboard: widget not found")

	// ErrSettingNotFound is returned when a requested setting does not exist in a contributor.
	ErrSettingNotFound = errors.New("dashboard: setting not found")
)
