package forge

// DebugMessageType identifies the kind of debug message sent over the WebSocket.
type DebugMessageType string

const (
	DebugMsgSnapshot  DebugMessageType = "snapshot"  // full state on WS connect
	DebugMsgMetrics   DebugMessageType = "metrics"   // periodic metrics tick
	DebugMsgHealth    DebugMessageType = "health"    // periodic health tick
	DebugMsgLifecycle DebugMessageType = "lifecycle" // lifecycle phase event
	DebugMsgPong      DebugMessageType = "pong"
)

// DebugMessage is the envelope for all WebSocket debug messages.
type DebugMessage struct {
	Type      DebugMessageType `json:"type"`
	Timestamp int64            `json:"ts"`
	AppName   string           `json:"app"`
	Payload   any              `json:"payload"`
}

// DebugSnapshot is the full state payload sent when a WS client connects.
type DebugSnapshot struct {
	App        DebugAppInfo       `json:"app"`
	Config     map[string]any     `json:"config"`
	Services   []string           `json:"services"`
	Routes     []DebugRoute       `json:"routes"`
	Extensions []DebugExtInfo     `json:"extensions"`
	Health     *DebugHealth       `json:"health,omitempty"`
}

// DebugAppInfo contains basic application metadata.
type DebugAppInfo struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Environment string `json:"environment"`
	HTTPAddr    string `json:"http_addr"`
	DebugAddr   string `json:"debug_addr"`
	UptimeMs    int64  `json:"uptime_ms"`
}

// DebugRoute describes a single registered HTTP route.
type DebugRoute struct {
	Name        string   `json:"name,omitempty"`
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Tags        []string `json:"tags,omitempty"`
	Summary     string   `json:"summary,omitempty"`
	Description string   `json:"description,omitempty"`
}

// DebugExtInfo describes a registered extension.
type DebugExtInfo struct {
	Name         string   `json:"name"`
	Version      string   `json:"version"`
	Description  string   `json:"description"`
	Dependencies []string `json:"dependencies"`
	Healthy      bool     `json:"healthy"`
}

// DebugHealth contains health check results.
type DebugHealth struct {
	Overall string                      `json:"overall"`
	Checks  map[string]DebugCheckResult `json:"checks"`
}

// DebugCheckResult is one health check's result.
type DebugCheckResult struct {
	Status     string `json:"status"`
	Message    string `json:"message,omitempty"`
	ResponseMs int64  `json:"response_ms,omitempty"`
}

// DebugMetrics wraps a Prometheus text-format metrics payload.
type DebugMetrics struct {
	Raw string `json:"raw"`
}

// DebugServerEntry is a single entry in ~/.forge/debug-servers.json.
type DebugServerEntry struct {
	PID          int    `json:"pid"`
	AppName      string `json:"app_name"`
	AppVersion   string `json:"app_version"`
	DebugAddr    string `json:"debug_addr"`
	AppAddr      string `json:"app_addr"`
	WorkspaceDir string `json:"workspace_dir"`
	StartedAt    string `json:"started_at"`
}
