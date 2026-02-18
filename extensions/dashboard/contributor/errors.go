package contributor

import "errors"

var (
	// ErrContributorExists is returned when a contributor with the same name is already registered.
	ErrContributorExists = errors.New("dashboard: contributor already registered")

	// ErrContributorNotFound is returned when a contributor is not found in the registry.
	ErrContributorNotFound = errors.New("dashboard: contributor not found")
)
