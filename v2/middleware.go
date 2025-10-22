package forge

import (
	"github.com/xraph/forge/v2/internal/router"
)

// MiddlewareFunc is a convenience type for middleware functions
// that want to explicitly call the next handler
type MiddlewareFunc = router.MiddlewareFunc
