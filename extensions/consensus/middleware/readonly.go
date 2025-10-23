package middleware

import (
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/consensus/internal"
)

// ReadOnlyMiddleware routes read-only requests to any node
type ReadOnlyMiddleware struct {
	raftNode internal.RaftNode
	logger   forge.Logger
}

// NewReadOnlyMiddleware creates read-only middleware
func NewReadOnlyMiddleware(raftNode internal.RaftNode, logger forge.Logger) *ReadOnlyMiddleware {
	return &ReadOnlyMiddleware{
		raftNode: raftNode,
		logger:   logger,
	}
}

// Handle allows read-only requests on any node
func (rom *ReadOnlyMiddleware) Handle() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// Check if request is read-only (GET, HEAD)
		if isReadOnlyMethod(ctx.Request().Method) {
			// Allow read-only requests on any node
			rom.logger.Debug("read-only request allowed",
				forge.F("method", ctx.Request().Method),
				forge.F("path", ctx.Request().URL.Path),
			)
			return nil
		}

		// For write requests, must be leader
		if !rom.raftNode.IsLeader() {
			return ctx.JSON(403, map[string]string{
				"error": "write requests must go to leader",
			})
		}

		return nil
	}
}

// HandleStrict enforces leadership even for reads
func (rom *ReadOnlyMiddleware) HandleStrict() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// All requests must go to leader
		if !rom.raftNode.IsLeader() {
			return ctx.JSON(403, map[string]string{
				"error": "all requests must go to leader",
			})
		}

		return nil
	}
}

// HandleWithStaleReads allows stale reads on followers
func (rom *ReadOnlyMiddleware) HandleWithStaleReads() func(forge.Context) error {
	return func(ctx forge.Context) error {
		// Check if request is read-only
		if isReadOnlyMethod(ctx.Request().Method) {
			// Check if client allows stale reads
			allowStale := ctx.Query("allow_stale") == "true" ||
				ctx.Header("X-Allow-Stale") == "true"

			if allowStale {
				rom.logger.Debug("stale read allowed",
					forge.F("method", ctx.Request().Method),
					forge.F("path", ctx.Request().URL.Path),
				)
				ctx.Set("stale_read", true)
				return nil
			}

			// If not allowing stale, must be leader
			if !rom.raftNode.IsLeader() {
				return ctx.JSON(403, map[string]string{
					"error": "linearizable reads must go to leader",
					"hint":  "add ?allow_stale=true for stale reads",
				})
			}

			return nil
		}

		// Write requests must go to leader
		if !rom.raftNode.IsLeader() {
			return ctx.JSON(403, map[string]string{
				"error": "write requests must go to leader",
			})
		}

		return nil
	}
}

// isReadOnlyMethod checks if HTTP method is read-only
func isReadOnlyMethod(method string) bool {
	method = strings.ToUpper(method)
	return method == "GET" || method == "HEAD" || method == "OPTIONS"
}
