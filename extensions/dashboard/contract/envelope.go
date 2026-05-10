// envelope.go
package contract

import "encoding/json"

// Kind discriminates request/response semantics on the wire.
// A kind is enforced against the intent's declared Capability at dispatch time.
type Kind string

const (
	KindGraph     Kind = "graph"
	KindQuery     Kind = "query"
	KindCommand   Kind = "command"
	KindSubscribe Kind = "subscribe"
)

// Request is the wire envelope for POST /api/dashboard/{envelope}.
type Request struct {
	Envelope       string          `json:"envelope"`
	Kind           Kind            `json:"kind"`
	Contributor    string          `json:"contributor"`
	Intent         string          `json:"intent"`
	IntentVersion  int             `json:"intentVersion,omitempty"`
	Payload        json.RawMessage `json:"payload,omitempty"`
	Params         map[string]any  `json:"params,omitempty"`
	Context        RequestContext  `json:"context"`
	CSRF           string          `json:"csrf,omitempty"`
	IdempotencyKey string          `json:"idempotencyKey,omitempty"`
}

// RequestContext carries route + correlation metadata. Always populated by the shell.
type RequestContext struct {
	Route         string `json:"route,omitempty"`
	CorrelationID string `json:"correlationID,omitempty"`
}

// Response is the wire envelope for successful POST responses.
type Response struct {
	OK       bool            `json:"ok"`
	Envelope string          `json:"envelope"`
	Kind     Kind            `json:"kind"`
	Data     json.RawMessage `json:"data,omitempty"`
	Meta     ResponseMeta    `json:"meta"`
}

// ResponseMeta carries cross-cutting metadata (versioning, caching, invalidation).
//
// RouteParams is populated by graph responses for routes that contain :name
// placeholders (e.g. /traces/:id). The map is keyed by placeholder name and
// holds the matched URL value. Slice (j) introduced this so the shell can
// resolve `route.<name>` in payload bindings.
type ResponseMeta struct {
	IntentVersion int               `json:"intentVersion,omitempty"`
	Deprecation   *Deprecation      `json:"deprecation,omitempty"`
	CacheControl  *CacheHint        `json:"cacheControl,omitempty"`
	Invalidates   []string          `json:"invalidates,omitempty"`
	RouteParams   map[string]string `json:"routeParams,omitempty"`
}

// Deprecation surfaces a "this version will be removed" hint to the shell.
type Deprecation struct {
	IntentVersion int    `json:"intentVersion"`
	RemoveAfter   string `json:"removeAfter"`
}

// CacheHint communicates how long the shell can serve stale data for a query.
type CacheHint struct {
	StaleTime string `json:"staleTime,omitempty"`
}

// ErrorResponse is the wire envelope for failed POST responses.
type ErrorResponse struct {
	OK       bool   `json:"ok"`
	Envelope string `json:"envelope"`
	Error    *Error `json:"error"`
}

// StreamEvent is the SSE payload for a single subscription event.
type StreamEvent struct {
	Intent  string           `json:"intent"`
	Mode    SubscriptionMode `json:"mode"`
	Payload json.RawMessage  `json:"payload"`
	Seq     uint64           `json:"seq"`
}

// SubscriptionMode is how the client integrates events into local state.
type SubscriptionMode string

const (
	ModeReplace       SubscriptionMode = "replace"
	ModeAppend        SubscriptionMode = "append"
	ModeSnapshotDelta SubscriptionMode = "snapshot+delta"
)
