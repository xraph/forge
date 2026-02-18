package handlers

import (
	"github.com/xraph/forge/extensions/dashboard/collector"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// Config mirrors the dashboard config fields needed by handlers.
type Config struct {
	BasePath       string
	Title          string
	Theme          string
	CustomCSS      string
	EnableExport   bool
	EnableRealtime bool
	EnableSearch   bool
	EnableSettings bool
	EnableBridge   bool
	ExportFormats  []string
}

// Deps holds shared dependencies for all handlers.
type Deps struct {
	Registry  *contributor.ContributorRegistry
	Collector *collector.DataCollector
	History   *collector.DataHistory
	Config    Config
}
