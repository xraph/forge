// Package register imports scheduler and storage implementations to register them.
// Import this package in your main.go to ensure schedulers and storage are available:
//
//	import _ "github.com/xraph/forge/extensions/cron/register"
package register

import (
	// Import scheduler implementations for registration.
	_ "github.com/xraph/forge/extensions/cron/scheduler"

	// Import storage implementations for registration.
	_ "github.com/xraph/forge/extensions/cron/storage"
)
