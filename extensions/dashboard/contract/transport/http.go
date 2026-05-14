// http.go
package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/security"
)

// Dispatcher routes a fully-validated request to an intent implementation.
// Slice (c) provides the binding from intent name to actual handlers.
type Dispatcher interface {
	Dispatch(ctx context.Context, in contract.Request, p contract.Principal) (json.RawMessage, contract.ResponseMeta, error)
}

// NilDispatcher is the safe default when no real Dispatcher has been wired.
// Every dispatch returns a CodeUnavailable contract error so that callers see
// a clear, kind-agnostic failure instead of a nil panic.
type NilDispatcher struct{}

// Dispatch implements Dispatcher.
func (NilDispatcher) Dispatch(_ context.Context, _ contract.Request, _ contract.Principal) (json.RawMessage, contract.ResponseMeta, error) {
	return nil, contract.ResponseMeta{}, &contract.Error{Code: contract.CodeUnavailable, Message: "no dispatcher configured"}
}

// supportedEnvelopes is the set this slice's handler understands.
var supportedEnvelopes = map[string]bool{"v1": true}

// NewHandler returns the POST /api/dashboard/{envelope} handler.
func NewHandler(reg contract.Registry, wreg contract.WardenRegistry, disp Dispatcher, audit contract.AuditEmitter) http.Handler {
	if disp == nil {
		disp = NilDispatcher{}
	}
	if audit == nil {
		audit = contract.NoopAuditEmitter{}
	}
	return &handler{reg: reg, wreg: wreg, disp: disp, audit: audit}
}

// NewHandlerWithCSRF is NewHandler plus a CSRFManager for command validation.
// When mgr is non-nil, command envelopes whose CSRF token does not validate
// return CodeUnauthenticated. Pass nil to skip CSRF (preserves the slice-(a)
// behaviour for tests and rollout opt-out).
func NewHandlerWithCSRF(reg contract.Registry, wreg contract.WardenRegistry, disp Dispatcher, audit contract.AuditEmitter, mgr *security.CSRFManager) http.Handler {
	h := NewHandler(reg, wreg, disp, audit).(*handler)
	h.csrfMgr = mgr
	return h
}

type handler struct {
	reg     contract.Registry
	wreg    contract.WardenRegistry
	disp    Dispatcher
	audit   contract.AuditEmitter
	csrfMgr *security.CSRFManager // optional; nil disables CSRF validation
}

// graphPayload is the payload shape for kind=graph requests.
// The shell sends `{ "route": "/some/path" }` and expects the merged graph
// for that route in the response Data plus extracted :name params (if any)
// in Meta.RouteParams. Slice (j) introduced this; before it, the graph kind
// 404'd because page.shell isn't a registered intent — it's a vocabulary
// marker for slot validation only.
type graphPayload struct {
	Route string `json:"route"`
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, &contract.Error{Code: contract.CodeBadRequest, Message: "POST required"})
		return
	}
	defer r.Body.Close()
	var req contract.Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: "invalid JSON: " + err.Error()})
		return
	}
	if !supportedEnvelopes[req.Envelope] {
		writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeUnsupportedVersion, Message: "envelope " + req.Envelope + " unsupported"})
		return
	}
	if err := validateKind(req); err != nil {
		writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: err.Error()})
		return
	}
	// Graph dispatches don't go through the dispatcher's intent table — page.shell
	// is a vocabulary root, not a registered handler. Resolve straight against the
	// merged manifest graph via GraphBuilder, with visibleWhen filtering applied.
	if req.Kind == contract.KindGraph {
		h.serveGraph(w, r, req)
		return
	}
	in, ok := h.reg.Intent(req.Contributor, req.Intent, intentVersionOrHighest(h.reg, req))
	if !ok {
		writeError(w, http.StatusNotFound, &contract.Error{Code: contract.CodeNotFound, Message: "intent " + req.Intent + " not registered"})
		return
	}
	if !kindMatchesCapability(req.Kind, in.Capability) {
		writeError(w, http.StatusBadRequest, &contract.Error{
			Code:    contract.CodeBadRequest,
			Message: "kind " + string(req.Kind) + " does not match intent capability " + string(in.Capability),
		})
		return
	}
	if req.Kind == contract.KindCommand {
		if req.IdempotencyKey == "" || req.CSRF == "" {
			writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: "command requires csrf and idempotencyKey"})
			return
		}
		if h.csrfMgr != nil && !h.csrfMgr.ValidateToken(req.CSRF) {
			writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodeUnauthenticated, Message: "csrf token invalid"})
			return
		}
	}

	user := dashauth.UserFromContext(r.Context())
	p := contract.PrincipalFor(user)

	if !in.Requires.Allow(user, nil) {
		writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodePermissionDenied})
		return
	}
	// Warden second pass when declared
	if in.Requires.Warden != "" {
		warden, ok := h.wreg.Get(in.Requires.Warden)
		if !ok {
			writeError(w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "warden not registered"})
			return
		}
		dec, err := warden.Authorize(r.Context(), p, contract.Action{
			Contributor: req.Contributor, Intent: req.Intent, Kind: req.Kind, Capability: in.Capability, Resource: req.Params,
		})
		if err != nil || !dec.Allow {
			writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodePermissionDenied, Message: dec.Reason})
			return
		}
	}

	t0 := time.Now()
	// Stash the live ResponseWriter + Request on ctx so command handlers that
	// legitimately need to touch HTTP (e.g. authsome's auth.login issuing a
	// Set-Cookie) can reach them via dashauth.ResponseWriterFromContext. Pure
	// data handlers ignore them. Slice (l) added this for the auth extension
	// integration; widening to graph/query is harmless because those handlers
	// already get a fresh copy of r.Context() and won't accidentally read it.
	dispatchCtx := dashauth.WithHTTP(r.Context(), w, r)
	data, meta, err := h.disp.Dispatch(dispatchCtx, req, p)
	latency := time.Since(t0)

	emitAudit(h.audit, req, in, p, err, latency)

	if err != nil {
		writeError(w, http.StatusInternalServerError, asContractError(err))
		return
	}
	writeOK(w, contract.Response{
		OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta,
	})
}

// serveGraph handles kind=graph requests. The payload carries `{route}`; the
// builder walks the contributor's merged graph, applies visibleWhen filters
// against the principal, and surfaces any :name route params in Meta.
func (h *handler) serveGraph(w http.ResponseWriter, r *http.Request, req contract.Request) {
	var pl graphPayload
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &pl); err != nil {
			writeError(w, http.StatusBadRequest, &contract.Error{Code: contract.CodeBadRequest, Message: "graph payload: " + err.Error()})
			return
		}
	}
	if pl.Route == "" {
		// fall back to the request context's route if the payload omitted it;
		// the shell sometimes sends only the latter.
		pl.Route = req.Context.Route
	}
	user := dashauth.UserFromContext(r.Context())
	p := contract.PrincipalFor(user)

	builder := contract.NewGraphBuilder(h.reg, h.wreg)
	node, params, err := builder.BuildWithParams(r.Context(), req.Contributor, pl.Route, p)
	if err != nil {
		ce := mapBuildError(err)
		writeError(w, statusForBuildError(ce), ce)
		return
	}
	data, mErr := json.Marshal(node)
	if mErr != nil {
		writeError(w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "graph encode: " + mErr.Error()})
		return
	}
	meta := contract.ResponseMeta{}
	if len(params) > 0 {
		meta.RouteParams = params
	}
	writeOK(w, contract.Response{
		OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta,
	})
}

// mapBuildError translates GraphBuilder errors into wire contract errors.
func mapBuildError(err error) *contract.Error {
	switch {
	case errors.Is(err, contract.ErrNotFound):
		return &contract.Error{Code: contract.CodeNotFound, Message: err.Error()}
	case errors.Is(err, contract.ErrPermissionDenied):
		return &contract.Error{Code: contract.CodePermissionDenied, Message: err.Error()}
	default:
		return &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
	}
}

// statusForBuildError picks the HTTP status that mirrors the contract error
// code. The wire envelope carries the canonical code regardless; the status
// is for HTTP-aware infrastructure (proxies, logs, browser fetch handlers).
func statusForBuildError(ce *contract.Error) int {
	switch ce.Code {
	case contract.CodeNotFound:
		return http.StatusNotFound
	case contract.CodePermissionDenied:
		return http.StatusForbidden
	default:
		return http.StatusInternalServerError
	}
}

func validateKind(req contract.Request) error {
	switch req.Kind {
	case contract.KindGraph, contract.KindQuery, contract.KindCommand:
		return nil
	case contract.KindSubscribe:
		return errKind("subscribe is GET-only on /stream")
	}
	return errKind("unknown kind " + string(req.Kind))
}

func errKind(msg string) error { return &contract.Error{Code: contract.CodeBadRequest, Message: msg} }

func kindMatchesCapability(k contract.Kind, c contract.Capability) bool {
	switch k {
	case contract.KindCommand:
		return c == contract.CapWrite
	case contract.KindQuery:
		return c == contract.CapRead
	case contract.KindGraph:
		return c == contract.CapRender
	}
	return false
}

func intentVersionOrHighest(reg contract.Registry, req contract.Request) int {
	if req.IntentVersion != 0 {
		return req.IntentVersion
	}
	v, _ := reg.HighestVersion(req.Contributor, req.Intent)
	return v
}

func emitAudit(em contract.AuditEmitter, req contract.Request, in contract.Intent, p contract.Principal, dispErr error, lat time.Duration) {
	if in.Kind != contract.IntentKindCommand {
		return
	}
	if in.Audit != nil && !*in.Audit {
		return
	}
	user := ""
	if p.User != nil {
		user = p.User.Subject
	}
	result := "ok"
	if dispErr != nil {
		result = "error"
	}
	em.Emit(context.Background(), contract.AuditRecord{
		Time: time.Now(), Contributor: req.Contributor, Intent: req.Intent,
		IntentVersion: in.Version, User: user, Result: result, LatencyMs: lat.Milliseconds(),
		CorrelationID: req.Context.CorrelationID,
	})
}

func asContractError(err error) *contract.Error {
	if e, ok := err.(*contract.Error); ok {
		return e
	}
	return &contract.Error{Code: contract.CodeInternal, Message: err.Error()}
}

func writeOK(w http.ResponseWriter, r contract.Response) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(r)
}

func writeError(w http.ResponseWriter, status int, e *contract.Error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(contract.ErrorResponse{
		OK: false, Envelope: "v1", Error: e,
	})
}
