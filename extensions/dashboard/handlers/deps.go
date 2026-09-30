package handlers

import (
	"github.com/xraph/forge/extensions/dashboard/collector"
)

// Deps holds the data sources the export handlers read from.
type Deps struct {
	Collector *collector.DataCollector
}
