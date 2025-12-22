package cron

import "github.com/xraph/forge/extensions/cron/core"

// Storage is the interface for job and execution persistence.
// This is re-exported from core to avoid import cycles.
// Storage implementations should be in the storage package.
type Storage = core.Storage
