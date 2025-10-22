package forge

// WithMetrics enables metrics collection
func WithMetrics(config MetricsConfig) RouterOption {
	return &metricsOption{config: config}
}

type metricsOption struct {
	config MetricsConfig
}

func (o *metricsOption) Apply(cfg *routerConfig) {
	cfg.metricsConfig = &o.config
}

// WithHealth enables health checks
func WithHealth(config HealthConfig) RouterOption {
	return &healthOption{config: config}
}

type healthOption struct {
	config HealthConfig
}

func (o *healthOption) Apply(cfg *routerConfig) {
	cfg.healthConfig = &o.config
}

// // setupObservability initializes observability features
// func (r *router) setupObservability() {
// 	// Initialize metrics
// 	if r.metricsConfig != nil && r.metricsConfig.Enabled {
// 		if r.metrics == nil {
// 			r.metrics = NewMetrics(r.metricsConfig, r.logger)
// 		}
// 		r.registerMetricsEndpoint()
// 	}
//
// 	// Initialize health manager
// 	if r.healthConfig != nil && r.healthConfig.Enabled {
// 		if r.healthManager == nil {
// 			r.healthManager = NewHealthManager(r.healthConfig, r.logger, r.metrics, r.container)
// 		}
// 		r.registerHealthEndpoints()
// 	}
// }
//
// // registerMetricsEndpoint registers the metrics endpoint
// func (r *router) registerMetricsEndpoint() {
// 	if r.metricsConfig == nil || !r.metricsConfig.Enabled {
// 		return
// 	}
//
// 	handler := func(w http.ResponseWriter, req *http.Request) {
// 		data, err := r.metrics.Export()
// 		if err != nil {
// 			http.Error(w, "Failed to export metrics", http.StatusInternalServerError)
// 			return
// 		}
//
// 		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
// 		w.WriteHeader(http.StatusOK)
// 		w.Write(data)
// 	}
//
// 	r.register(http.MethodGet, r.metricsConfig.MetricsPath, handler)
// }

// // registerHealthEndpoints registers health check endpoints
// func (r *router) registerHealthEndpoints() {
// 	if r.healthConfig == nil || !r.healthConfig.Enabled {
// 		return
// 	}
//
// 	// Main health endpoint
// 	r.register(http.MethodGet, r.healthConfig.HealthPath, func(w http.ResponseWriter, req *http.Request) {
// 		ctx := &ctx{
// 			request:       req,
// 			response:      w,
// 			container:     r.container,
// 			metrics:       r.metrics,
// 			healthManager: r.healthManager,
// 		}
//
// 		report := r.healthManager.Check(req.Context())
//
// 		// Set status code based on health
// 		statusCode := http.StatusOK
// 		if report.Overall == HealthStatusUnhealthy {
// 			statusCode = http.StatusServiceUnavailable
// 		}
//
// 		ctx.JSON(statusCode, report)
// 	})
//
// 	// Liveness probe
// 	r.register(http.MethodGet, r.healthConfig.LivenessPath, func(w http.ResponseWriter, req *http.Request) {
// 		ctx := &ctx{
// 			request:       req,
// 			response:      w,
// 			container:     r.container,
// 			metrics:       r.metrics,
// 			healthManager: r.healthManager,
// 		}
// 		ctx.JSON(http.StatusOK, map[string]string{"status": "alive"})
// 	})
//
// 	// Readiness probe
// 	r.register(http.MethodGet, r.healthConfig.ReadinessPath, func(w http.ResponseWriter, req *http.Request) {
// 		ctx := &ctx{
// 			request:       req,
// 			response:      w,
// 			container:     r.container,
// 			metrics:       r.metrics,
// 			healthManager: r.healthManager,
// 		}
//
// 		report := r.healthManager.Check(req.Context())
//
// 		if report.Overall == HealthStatusHealthy {
// 			ctx.JSON(http.StatusOK, map[string]string{"status": "ready"})
// 		} else {
// 			ctx.JSON(http.StatusServiceUnavailable, map[string]string{
// 				"status":  "not ready",
// 				"message": "one or more services unhealthy",
// 			})
// 		}
// 	})
// }
