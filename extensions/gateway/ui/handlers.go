package ui

import (
	"github.com/a-h/templ"

	"github.com/xraph/forgeui/router"
)

// GatewayDashboardPageHandler returns a ForgeUI page handler for the gateway dashboard.
func GatewayDashboardPageHandler(title, basePath string, enableRealtime bool) router.PageHandler {
	return func(ctx *router.PageContext) (templ.Component, error) {
		return GatewayDashboardPage(title, basePath, enableRealtime), nil
	}
}
