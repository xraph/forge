package dispatcher

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

const (
	bindingFormat        = "forge.dashboard.idempotency"
	bindingVersion       = 1
	bindingMaxDepth      = 64
	bindingMaxNodes      = 100000
	bindingMaxBytes      = 8 << 20
	bindingMaxScopeBytes = 64 << 10
	// ReasonBindingConflict refuses ambiguous or differently bound cache entries.
	ReasonBindingConflict = "idempotency.binding_conflict"
	// ReasonClaimRequired means a fresh keyed command needs an atomic cache backend.
	ReasonClaimRequired = "idempotency.claim_required"
)

// IdempotencyScope adds trusted application scope to the mandatory request and
// full-principal binding. Callbacks run in registration order after admission,
// before cache access and again after Claim. Supply deterministic, repeatable
// bytes from validated authority or static composition, with your scope version.
// Callbacks must not mutate domain state. A nil callback rejects registration.
func IdempotencyScope(fn func(context.Context, contract.Request, contract.Principal) ([]byte, error)) RegisterOption {
	return func(e *handlerEntry) {
		if fn == nil {
			e.registrationErr = errors.New("dispatcher: nil IdempotencyScope callback")

			return
		}

		e.idempotencyScopes = append(e.idempotencyScopes, fn)
	}
}

func errBindingConflict() *contract.Error {
	return &contract.Error{Code: contract.CodeConflict, Message: "idempotency record does not match this operation", Details: map[string]any{ReasonDetail: ReasonBindingConflict}}
}

func errClaimRequired() *contract.Error {
	return &contract.Error{Code: contract.CodeUnavailable, Message: "atomic idempotency claims are required", Details: map[string]any{ReasonDetail: ReasonClaimRequired}}
}

func requestBinding(ctx context.Context, req contract.Request, p contract.Principal, entry handlerEntry) (string, error) {
	c := bindingEncoder{}
	c.scalar("domain", bindingFormat+"/binding/1")

	for _, value := range []any{req.Envelope, req.Contributor, req.Intent, req.IntentVersion, string(req.Kind), req.Payload, req.Params} {
		c.value(value, 0)
	}

	c.value(p.User != nil, 0)

	if u := p.User; u != nil {
		for _, value := range []any{u.Subject, u.DisplayName, u.Email, u.AvatarURL, u.ProviderName, u.Roles, u.Scopes, u.Claims, u.Metadata} {
			c.value(value, 0)
		}
	}

	c.value(p.Claims, 0)
	c.value(len(entry.idempotencyScopes), 0)

	for _, scope := range entry.idempotencyScopes {
		if c.err != nil {
			break
		}

		value, err := scope(ctx, req, p)
		if err != nil {
			return "", mapDispatchError(err)
		}

		if len(value) > bindingMaxScopeBytes {
			c.err = errors.New("scope exceeds binding limit")

			break
		}

		c.value(value, 0)
	}

	if c.err != nil {
		return "", &contract.Error{Code: contract.CodeBadRequest, Message: "unsupported or excessive idempotency binding input"}
	}

	digest := sha256.Sum256(c.buf.Bytes())

	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

// Every scalar is type-tagged and length-framed. Containers include their type,
// nil bit and length. Only the exact types below are accepted, without invoking
// user marshalers. Limits apply before allocation or descent into child values.
type bindingEncoder struct {
	buf   bytes.Buffer
	nodes int
	err   error
}

func (c *bindingEncoder) write(value []byte) {
	if c.err != nil {
		return
	}

	if len(value) > bindingMaxBytes-c.buf.Len() {
		c.err = errors.New("binding byte limit")

		return
	}

	_, _ = c.buf.Write(value)
}

func (c *bindingEncoder) scalar(tag, value string) {
	c.write([]byte(tag))
	c.write([]byte{0})

	var size [8]byte
	binary.BigEndian.PutUint64(size[:], uint64(len(value)))
	c.write(size[:])
	// Check length before converting a potentially large input to bytes.
	if len(value) > bindingMaxBytes-c.buf.Len() {
		c.err = errors.New("binding byte limit")

		return
	}

	c.write([]byte(value))
}

func (c *bindingEncoder) container(tag string, nilValue bool, length, depth int) bool {
	if depth >= bindingMaxDepth || length > bindingMaxNodes-c.nodes {
		c.err = errors.New("binding container limit")

		return false
	}

	c.scalar(tag, strconv.FormatBool(nilValue)+":"+strconv.Itoa(length))

	return c.err == nil
}

func (c *bindingEncoder) value(value any, depth int) {
	if c.err != nil {
		return
	}

	c.nodes++
	if c.nodes > bindingMaxNodes {
		c.err = errors.New("binding node limit")

		return
	}

	switch v := value.(type) {
	case nil:
		c.scalar("nil", "")
	case bool:
		c.scalar("bool", strconv.FormatBool(v))
	case string:
		if len(v) > bindingMaxBytes-c.buf.Len() {
			c.err = errors.New("binding byte limit")

			return
		}

		if !utf8.ValidString(v) {
			c.err = errors.New("invalid UTF-8")

			return
		}

		c.scalar("string", v)
	case int, int8, int16, int32, int64:
		c.scalar(fmt.Sprintf("%T", v), strconv.FormatInt(reflect.ValueOf(v).Int(), 10))
	case uint, uint8, uint16, uint32, uint64:
		c.scalar(fmt.Sprintf("%T", v), strconv.FormatUint(reflect.ValueOf(v).Uint(), 10))
	case float32:
		if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
			c.err = errors.New("non-finite float")

			return
		}

		c.scalar("float32", strconv.FormatUint(uint64(math.Float32bits(v)), 16))
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			c.err = errors.New("non-finite float")

			return
		}

		c.scalar("float64", strconv.FormatUint(math.Float64bits(v), 16))
	case json.Number:
		// Marshal validates the JSON number grammar without a float conversion.
		if len(v) > bindingMaxBytes-c.buf.Len() {
			c.err = errors.New("binding byte limit")

			return
		}

		if _, err := json.Marshal(v); err != nil || v == "" {
			c.err = errors.New("invalid JSON number")

			return
		}

		c.scalar("number", string(v))
	case json.RawMessage:
		c.scalar("raw-nil", strconv.FormatBool(v == nil))

		if len(v) > bindingMaxBytes-c.buf.Len() {
			c.err = errors.New("binding byte limit")

			return
		}

		c.scalar("raw", string(v))
	case []byte:
		c.scalar("bytes-nil", strconv.FormatBool(v == nil))

		if len(v) > bindingMaxBytes-c.buf.Len() {
			c.err = errors.New("binding byte limit")

			return
		}

		c.scalar("bytes", string(v))
	case []string:
		if !c.container("strings", v == nil, len(v), depth) {
			return
		}

		for _, child := range v {
			c.value(child, depth+1)
		}
	case []any:
		if !c.container("values", v == nil, len(v), depth) {
			return
		}

		for _, child := range v {
			c.value(child, depth+1)
		}
	case map[string]string:
		if !c.container("string-map", v == nil, len(v)*2, depth) {
			return
		}

		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}

		slices.Sort(keys)

		for _, key := range keys {
			c.value(key, depth+1)
			c.value(v[key], depth+1)
		}
	case map[string]any:
		if !c.container("value-map", v == nil, len(v)*2, depth) {
			return
		}

		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}

		slices.Sort(keys)

		for _, key := range keys {
			c.value(key, depth+1)
			c.value(v[key], depth+1)
		}
	default:
		c.err = errors.New("unsupported binding value type")
	}
}

type boundResponse struct {
	Format   string            `json:"format"`
	Version  int               `json:"version"`
	Binding  string            `json:"binding"`
	Response contract.Response `json:"response"`
}

// protocolObject checks exact field names, duplicate fields and trailing input.
// Data is opaque JSON; protocol objects are checked separately below.
func protocolObject(body []byte, required, optional []string) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(body))

	token, err := dec.Token()
	if err != nil || token != json.Delim('{') {
		return nil, errors.New("expected protocol object")
	}

	fields := make(map[string]json.RawMessage)

	for dec.More() {
		token, err = dec.Token()
		if err != nil {
			return nil, err
		}

		key, ok := token.(string)
		if !ok || (!slices.Contains(required, key) && !slices.Contains(optional, key)) {
			return nil, errors.New("unknown protocol field")
		}

		if _, exists := fields[key]; exists {
			return nil, errors.New("duplicate protocol field")
		}

		var value json.RawMessage
		if err = dec.Decode(&value); err != nil {
			return nil, err
		}

		fields[key] = value
	}

	if _, err = dec.Token(); err != nil {
		return nil, err
	}

	if _, err = dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing protocol input")
	}

	for _, key := range required {
		if raw, exists := fields[key]; !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil, errors.New("missing protocol field")
		}
	}

	return fields, nil
}

func decodeBoundResponse(body []byte, req contract.Request, binding string) (contract.Response, error) {
	var record boundResponse

	fields, err := protocolObject(body, []string{"format", "version", "binding", "response"}, nil)
	if err != nil {
		return record.Response, err
	}

	response, err := protocolObject(fields["response"], []string{"ok", "envelope", "kind", "meta"}, []string{"data"})
	if err != nil {
		return record.Response, err
	}

	meta, err := protocolObject(response["meta"], nil, []string{"intentVersion", "deprecation", "cacheControl", "invalidates"})
	if err != nil {
		return record.Response, err
	}

	if version, exists := meta["intentVersion"]; (req.IntentVersion != 0 && !exists) || bytes.Equal(bytes.TrimSpace(version), []byte("null")) {
		return record.Response, errors.New("missing intent version")
	}

	for field, keys := range map[string][]string{"deprecation": {"intentVersion", "removeAfter"}, "cacheControl": {"staleTime"}} {
		if raw, exists := meta[field]; exists && !bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			if _, err = protocolObject(raw, nil, keys); err != nil {
				return record.Response, err
			}
		}
	}

	if err = json.Unmarshal(body, &record); err != nil {
		return record.Response, err
	}

	if record.Format != bindingFormat || record.Version != bindingVersion || len(record.Binding) != 71 || !strings.HasPrefix(record.Binding, "sha256:") {
		return record.Response, errors.New("unsupported binding record")
	}

	for _, ch := range record.Binding[7:] {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return record.Response, errors.New("invalid binding digest")
		}
	}

	if record.Binding != binding || !record.Response.OK || record.Response.Kind != req.Kind || record.Response.Envelope != req.Envelope || record.Response.Meta.IntentVersion != req.IntentVersion {
		return record.Response, errors.New("binding mismatch")
	}

	return record.Response, nil
}

func answerCached(entry handlerEntry, cached *IdempotencyCached, req contract.Request, binding string) (json.RawMessage, contract.ResponseMeta, error) {
	if cached == nil {
		return nil, contract.ResponseMeta{}, errClaimFailed()
	}

	if entry.secret || (cached.Status == TombstoneStatus && len(cached.WireBody) == 0) {
		return nil, contract.ResponseMeta{}, errSecretNotKept()
	}

	if cached.Status == TombstoneStatus {
		if response, err := decodeBoundResponse(cached.WireBody, req, binding); err == nil {
			return response.Data, response.Meta, nil
		}
	}

	return nil, contract.ResponseMeta{}, errBindingConflict()
}
