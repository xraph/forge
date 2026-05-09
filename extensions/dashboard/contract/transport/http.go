// http.go
package transport

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	"github.com/xraph/forge/extensions/dashboard/contract"
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

type handler struct {
	reg   contract.Registry
	wreg  contract.WardenRegistry
	disp  Dispatcher
	audit contract.AuditEmitter
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
	data, meta, err := h.disp.Dispatch(r.Context(), req, p)
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
