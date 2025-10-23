package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/websocket"
	"github.com/xraph/forge"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins in development
	},
}

// handleIndex serves the main dashboard HTML
func (ds *DashboardServer) handleIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")

	html := ds.generateHTML()
	w.Write([]byte(html))
}

// handleAPIOverview returns overview data as JSON
func (ds *DashboardServer) handleAPIOverview(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	overview := ds.collector.CollectOverview(r.Context())
	ds.respondJSON(w, overview)
}

// handleAPIHealth returns health check data as JSON
func (ds *DashboardServer) handleAPIHealth(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	health := ds.collector.CollectHealth(r.Context())
	ds.respondJSON(w, health)
}

// handleAPIMetrics returns metrics data as JSON
func (ds *DashboardServer) handleAPIMetrics(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	metrics := ds.collector.CollectMetrics(r.Context())
	ds.respondJSON(w, metrics)
}

// handleAPIServices returns service list as JSON
func (ds *DashboardServer) handleAPIServices(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	services := ds.collector.CollectServices(r.Context())
	ds.respondJSON(w, services)
}

// handleAPIHistory returns historical data as JSON
func (ds *DashboardServer) handleAPIHistory(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	history := ds.history.GetAll()
	ds.respondJSON(w, history)
}

// handleAPIServiceDetail returns detailed information about a specific service
func (ds *DashboardServer) handleAPIServiceDetail(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// Get service name from query parameter
	serviceName := r.URL.Query().Get("name")
	if serviceName == "" {
		ds.respondError(w, http.StatusBadRequest, "service name is required")
		return
	}

	detail := ds.collector.CollectServiceDetail(r.Context(), serviceName)
	ds.respondJSON(w, detail)
}

// handleAPIMetricsReport returns comprehensive metrics report
func (ds *DashboardServer) handleAPIMetricsReport(w http.ResponseWriter, r *http.Request) {
	ds.setCORSHeaders(w)

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	report := ds.collector.CollectMetricsReport(r.Context())
	ds.respondJSON(w, report)
}

// handleWebSocket handles WebSocket connections
func (ds *DashboardServer) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if ds.hub == nil {
		http.Error(w, "WebSocket not enabled", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		ds.logger.Error("websocket upgrade failed",
			forge.F("error", err),
		)
		return
	}

	client := NewClient(ds.hub, conn)
	ds.hub.register <- client

	// Start client pumps
	client.Start()
}

// setCORSHeaders sets CORS headers for API responses
func (ds *DashboardServer) setCORSHeaders(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
	w.Header().Set("Access-Control-Max-Age", "86400")
}

// respondJSON writes a JSON response
func (ds *DashboardServer) respondJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(data); err != nil {
		ds.logger.Error("failed to encode JSON response",
			forge.F("error", err),
		)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// respondError writes an error response
func (ds *DashboardServer) respondError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	response := map[string]interface{}{
		"error":  message,
		"status": status,
	}

	json.NewEncoder(w).Encode(response)
}
