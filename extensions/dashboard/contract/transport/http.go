package transport

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
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

// DefaultMaxBodyBytes caps the request envelope when no WithMaxBodyBytes
// option is given. The whole envelope is decoded before the intent's
// Requires predicate can run, so this cap is what bounds the work an
// unauthorized caller can make the server do.
const DefaultMaxBodyBytes int64 = 1 << 20

// HandlerOption configures the handler returned by NewHandler.
type HandlerOption func(*handler)

// WithMaxBodyBytes sets the largest request envelope the handler will read.
// A larger body is refused with 413 and CodeBadRequest. n <= 0 keeps
// DefaultMaxBodyBytes; there is no way to switch the cap off.
func WithMaxBodyBytes(n int64) HandlerOption {
	return func(h *handler) {
		if n > 0 {
			h.maxBody = n
		}
	}
}

// NewHandler returns the POST /api/dashboard/{envelope} handler.
func NewHandler(reg contract.Registry, wreg contract.WardenRegistry, disp Dispatcher, audit contract.AuditEmitter, opts ...HandlerOption) http.Handler {
	if disp == nil {
		disp = NilDispatcher{}
	}

	if audit == nil {
		audit = contract.NoopAuditEmitter{}
	}

	h := &handler{reg: reg, wreg: wreg, disp: disp, audit: audit, maxBody: DefaultMaxBodyBytes}
	for _, o := range opts {
		o(h)
	}

	return h
}

// NewHandlerWithCSRF is NewHandler plus a CSRFManager for command validation.
// When mgr is non-nil, command envelopes whose CSRF token does not validate
// return CodeUnauthenticated. Pass nil to skip CSRF (preserves the slice-(a)
// behaviour for tests and rollout opt-out).
func NewHandlerWithCSRF(reg contract.Registry, wreg contract.WardenRegistry, disp Dispatcher, audit contract.AuditEmitter, mgr *security.CSRFManager, opts ...HandlerOption) http.Handler {
	h := NewHandler(reg, wreg, disp, audit, opts...).(*handler)
	h.csrfMgr = mgr

	return h
}

type handler struct {
	reg     contract.Registry
	wreg    contract.WardenRegistry
	disp    Dispatcher
	audit   contract.AuditEmitter
	csrfMgr *security.CSRFManager // optional; nil disables CSRF validation
	maxBody int64                 // envelope size cap; always > 0
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, &contract.Error{Code: contract.CodeBadRequest, Message: "POST required"})

		return
	}
	defer r.Body.Close()
	// The intent name lives inside the envelope, so its Requires predicate
	// cannot run until the body is decoded. Cap the body first: any caller,
	// authorized or not, can otherwise make us read and decode without bound.
	// A declared length over the cap is refused without reading a byte.
	if r.ContentLength > h.maxBody {
		writeBodyTooLarge(w, h.maxBody)

		return
	}

	var req contract.Request
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, h.maxBody)).Decode(&req); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeBodyTooLarge(w, h.maxBody)

			return
		}

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
	// Normalize the resolved version onto the request so the dispatcher's
	// (contributor, intent, version) handler-map lookup matches what the
	// contributor actually registered. Without this, a client that omits
	// intentVersion (the field defaults to 0) finds the intent in the
	// registry via the "0 means highest" rule above but then hits
	// "handler {contributor}/{intent}@0 not registered" because the
	// dispatcher receives version 0 verbatim. The same normalization also
	// matters when this request is forwarded to a remote upstream via the
	// dispatcher's remote-fallback path — the upstream's transport reaches
	// the exact same dispatcher lookup against its local registry.
	req.IntentVersion = in.Version
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
		if err != nil {
			contractErr := asContractError(err)
			writeError(w, errorStatus(contractErr.Code), contractErr)

			return
		}

		if !dec.Allow {
			writeError(w, http.StatusForbidden, &contract.Error{Code: contract.CodePermissionDenied, Message: dec.Reason})

			return
		}
	}

	t0 := time.Now()
	// Stash the live ResponseWriter + Request on ctx so command handlers that
	// legitimately need to touch HTTP (e.g. authsome's auth.login issuing a
	// Set-Cookie) can reach them via dashauth.ResponseWriterFromContext. Pure
	// data handlers ignore them. Slice (l) added this for the auth extension
	// integration; widening to query/subscribe is harmless because those
	// handlers already get a fresh copy of r.Context() and won't accidentally
	// read it.
	dispatchCtx := dashauth.WithHTTP(r.Context(), w, r)
	data, meta, err := h.disp.Dispatch(dispatchCtx, req, p)
	latency := time.Since(t0)

	emitAudit(h.audit, req, in, p, err, latency)

	if err != nil {
		contractErr := asContractError(err)
		writeError(w, errorStatus(contractErr.Code), contractErr)

		return
	}

	if req.Kind == contract.KindCommand {
		invalidates := make([]string, 0, len(in.Invalidates)+len(meta.Invalidates))

		seen := make(map[string]bool)
		for _, intent := range append(append([]string(nil), in.Invalidates...), meta.Invalidates...) {
			if !seen[intent] {
				invalidates = append(invalidates, intent)
				seen[intent] = true
			}
		}

		meta.Invalidates = invalidates
	}

	writeOK(w, contract.Response{
		OK: true, Envelope: req.Envelope, Kind: req.Kind, Data: data, Meta: meta,
	})
}

func validateKind(req contract.Request) error {
	switch req.Kind {
	case contract.KindQuery, contract.KindCommand:
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
	var e *contract.Error
	if errors.As(err, &e) && e != nil {
		return e
	}

	return &contract.Error{Code: contract.CodeInternal, Message: "internal error"}
}

func errorStatus(code contract.ErrorCode) int {
	switch code {
	case contract.CodeBadRequest, contract.CodeUnsupportedVersion:
		return http.StatusBadRequest
	case contract.CodeUnauthenticated:
		return http.StatusUnauthorized
	case contract.CodePermissionDenied:
		return http.StatusForbidden
	case contract.CodeNotFound:
		return http.StatusNotFound
	case contract.CodeConflict:
		return http.StatusConflict
	case contract.CodeRateLimited:
		return http.StatusTooManyRequests
	case contract.CodeUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}

func writeOK(w http.ResponseWriter, r contract.Response) {
	body, err := json.Marshal(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "internal error"})

		return
	}

	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(append(body, '\n')); err != nil {
		return
	}
}

func writeBodyTooLarge(w http.ResponseWriter, limit int64) {
	writeError(w, http.StatusRequestEntityTooLarge, &contract.Error{
		Code:    contract.CodeBadRequest,
		Message: "request body exceeds " + strconv.FormatInt(limit, 10) + " bytes",
	})
}

func writeError(w http.ResponseWriter, status int, e *contract.Error) {
	body, err := json.Marshal(contract.ErrorResponse{
		OK: false, Envelope: "v1", Error: e,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, &contract.Error{Code: contract.CodeInternal, Message: "internal error"})

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if _, err := w.Write(append(body, '\n')); err != nil {
		return
	}
}
