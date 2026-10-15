// Package observability runs forge's local Grafana stack and renders the
// dashboard and alert rules that ship with the CLI.
package observability

import "embed"

//go:embed assets
var assets embed.FS
