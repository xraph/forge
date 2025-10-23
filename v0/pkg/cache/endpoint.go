package cache

import (
	"fmt"

	cachecore "github.com/xraph/forge/pkg/cache/core"
	"github.com/xraph/forge/pkg/common"
)

// CacheEndpointHandler provides HTTP endpoints for cache management
type CacheEndpointHandler struct {
	manager cachecore.CacheManager
	logger  common.Logger
	metrics common.Metrics
}

func NewCacheEndpointHandler(manager cachecore.CacheManager, logger common.Logger, metrics common.Metrics) *CacheEndpointHandler {
	return &CacheEndpointHandler{
		manager: manager,
		logger:  logger,
		metrics: metrics,
	}
}

// RegisterEndpoints registers cache management endpoints with the router
func (h *CacheEndpointHandler) RegisterEndpoints(router common.Router) error {
	// Cache stats endpoint
	if err := router.GET("/cache/stats", h.handleCacheStats); err != nil {
		return fmt.Errorf("failed to register cache stats endpoint: %w", err)
	}

	// Cache health endpoint
	if err := router.GET("/cache/health", h.handleCacheHealth); err != nil {
		return fmt.Errorf("failed to register cache health endpoint: %w", err)
	}

	// Cache invalidation endpoint
	if err := router.POST("/cache/invalidate", h.handleCacheInvalidate); err != nil {
		return fmt.Errorf("failed to register cache invalidation endpoint: %w", err)
	}

	// Cache warming endpoint
	if err := router.POST("/cache/warm", h.handleCacheWarm); err != nil {
		return fmt.Errorf("failed to register cache warming endpoint: %w", err)
	}

	// Cache backends list endpoint
	if err := router.GET("/cache/backends", h.handleCacheBackends); err != nil {
		return fmt.Errorf("failed to register cache backends endpoint: %w", err)
	}

	if h.logger != nil {
		h.logger.Info("cache management endpoints registered successfully")
	}

	return nil
}

// handleCacheStats returns cache statistics
func (h *CacheEndpointHandler) handleCacheStats(ctx common.Context) error {
	stats := h.manager.GetStats()
	combinedStats := h.manager.GetCombinedStats()

	response := map[string]interface{}{
		"combined": combinedStats,
		"backends": stats,
	}

	return ctx.JSON(200, response)
}

// handleCacheHealth performs cache health check
func (h *CacheEndpointHandler) handleCacheHealth(ctx common.Context) error {
	err := h.manager.OnHealthCheck(ctx.Request().Context())
	if err != nil {
		return ctx.JSON(503, map[string]interface{}{
			"status": "unhealthy",
			"error":  err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"status": "healthy",
	})
}

// handleCacheInvalidate handles cache invalidation requests
func (h *CacheEndpointHandler) handleCacheInvalidate(ctx common.Context) error {
	var request struct {
		Pattern string   `json:"pattern,omitempty"`
		Tags    []string `json:"tags,omitempty"`
	}

	// if err := ctx.Bind(&request); err != nil {
	// 	return ctx.JSON(400, map[string]interface{}{
	// 		"error": "invalid request body",
	// 	})
	// }

	var err error
	if request.Pattern != "" {
		err = h.manager.InvalidatePattern(ctx.Request().Context(), request.Pattern)
	} else if len(request.Tags) > 0 {
		err = h.manager.InvalidateByTags(ctx.Request().Context(), request.Tags)
	} else {
		return ctx.JSON(400, map[string]interface{}{
			"error": "either pattern or tags must be provided",
		})
	}

	if err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"status": "invalidated",
	})
}

// handleCacheWarm handles cache warming requests
func (h *CacheEndpointHandler) handleCacheWarm(ctx common.Context) error {
	var request struct {
		CacheName string `json:"cache_name,omitempty"`
	}

	// if err := ctx.Bind(&request); err != nil {
	// 	return ctx.JSON(400, map[string]interface{}{
	// 		"error": "invalid request body",
	// 	})
	// }

	cacheName := request.CacheName
	if cacheName == "" {
		// Get default cache name
		caches := h.manager.ListCaches()
		if len(caches) > 0 {
			cacheName = caches[0].Name()
		}
	}

	err := h.manager.WarmCache(ctx.Request().Context(), cacheName, WarmConfig{})
	if err != nil {
		return ctx.JSON(500, map[string]interface{}{
			"error": err.Error(),
		})
	}

	return ctx.JSON(200, map[string]interface{}{
		"status": "warming_started",
		"cache":  cacheName,
	})
}

// handleCacheBackends returns list of cache backends
func (h *CacheEndpointHandler) handleCacheBackends(ctx common.Context) error {
	backends := h.manager.ListCaches()

	response := map[string]interface{}{
		"backends": backends,
		"count":    len(backends),
	}

	return ctx.JSON(200, response)
}
