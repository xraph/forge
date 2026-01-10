package ui

import (
	g "maragu.dev/gomponents"

	"github.com/xraph/forgeui/router"
)

// DashboardPageHandler returns a ForgeUI page handler for the dashboard.
func DashboardPageHandler(title, basePath string, enableRealtime, enableExport bool) router.PageHandler {
	return func(ctx *router.PageContext) (g.Node, error) {
		return DashboardPage(title, basePath, enableRealtime, enableExport), nil
	}
}
