package ui

import (
	g "maragu.dev/gomponents"

	"github.com/xraph/forgeui/router"
)

// GatewayDashboardPageHandler returns a ForgeUI page handler for the gateway dashboard.
func GatewayDashboardPageHandler(title, basePath string, enableRealtime bool) router.PageHandler {
	return func(ctx *router.PageContext) (g.Node, error) {
		return GatewayDashboardPage(title, basePath, enableRealtime), nil
	}
}
