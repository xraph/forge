package cron

import "github.com/xraph/forge/extensions/cron/core"

// Scheduler is the interface for job schedulers.
// This is re-exported from core to avoid import cycles.
// Scheduler implementations should be in the scheduler package.
type Scheduler = core.Scheduler

