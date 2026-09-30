package dashboard

import "errors"

var (
	// ErrCollectorNotInitialized is returned when the data collector is not initialized.
	ErrCollectorNotInitialized = errors.New("dashboard: collector not initialized")
)
