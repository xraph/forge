package forge

import (
	"github.com/xraph/forge/internal/router"
)

// BunRouterAdapter wraps uptrace/bunrouter.
type BunRouterAdapter = router.BunRouterAdapter

// NewBunRouterAdapter creates a BunRouter adapter (default).
func NewBunRouterAdapter() RouterAdapter {
	return router.NewBunRouterAdapter()
}
