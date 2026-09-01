package routertest_test

import (
	"testing"

	"github.com/xraph/forge/internal/router"
	"github.com/xraph/forge/internal/router/forgemux"
	"github.com/xraph/forge/internal/router/routertest"
	"github.com/xraph/forge/internal/shared"
)

func TestConformance(t *testing.T) {
	routertest.RunConformance(t, "bunrouter", func() shared.RouterAdapter {
		return router.NewBunRouterAdapter()
	})

	routertest.RunConformance(t, "forgemux", func() shared.RouterAdapter {
		return forgemux.New()
	})
}
