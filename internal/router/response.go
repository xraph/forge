package router

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	"github.com/xraph/forge/internal/shared"
)

// This file turns a typed handler's response value into the JSON body.
//
// It replaces a path that processed every response twice. The router ran
// shared.ProcessResponseValueWithSensitive (headers, body unwrapping,
// sensitive-field cleaning) and then handed the result to ctx.JSON, which ran
// the same processing again in go-utils. With WithSensitiveFieldCleaning the
// whole response was deep-copied twice, and a body:"" field whose value had
// header or body tags of its own was unwrapped a second time. The router now
// processes once and writes the encoded body itself.
//
// The cleaning also built each struct copy from a zero value and filled in
// only the exported fields, so every unexported field was lost: a time.Time
// (all unexported fields) became 0001-01-01T00:00:00Z. A struct copy now
// starts from the original value, and a type that holds nothing to clean,
// such as time.Time, is not copied at all.

// writeTypedResponse processes resp (headers, body unwrapping, sensitive
// fields) and writes it as a 200 JSON response.
func writeTypedResponse(ctx Context, resp reflect.Value) {
	body := processResponse(ctx, resp)
	if body == nil {
		return
	}

	writeJSONBody(ctx, http.StatusOK, body)
}

// processResponse handles response struct tags:
//   - sets header:"..." fields as HTTP response headers,
//   - unwraps a body:"" field to return just its content,
//   - otherwise returns the whole value,
//   - cleans sensitive fields first when the route enables it.
func processResponse(ctx Context, resp reflect.Value) any {
	if resp.Kind() == reflect.Pointer && resp.IsNil() {
		return nil
	}

	cleanSensitive := false
	if b, ok := ctx.Get(ContextKeySensitiveFieldCleaning).(bool); ok {
		cleanSensitive = b
	}

	return processResponseValue(resp.Interface(), ctx.SetHeader, cleanSensitive)
}

// processResponseValue is processResponse on a plain value.
func processResponseValue(v any, setHeader func(name, value string), cleanSensitive bool) any {
	if v == nil {
		return nil
	}

	if cleanSensitive {
		v = cleanSensitiveFields(v)
	}

	rv := reflect.ValueOf(v)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return nil
		}

		rv = rv.Elem()
	}

	if rv.Kind() != reflect.Struct {
		return v
	}

	tags := responseTagsFor(rv.Type())

	for _, h := range tags.headers {
		if f := rv.Field(h.index); !f.IsZero() && setHeader != nil {
			setHeader(h.name, fmt.Sprint(f.Interface()))
		}
	}

	if tags.body >= 0 {
		return rv.Field(tags.body).Interface()
	}

	return v
}

// responseTags is where a response struct's header:"..." and body:"" fields
// are. Computed once per type.
type responseTags struct {
	headers []headerField
	body    int // index of the body:"" field, -1 if none
}

type headerField struct {
	index int
	name  string
}

var responseTagCache sync.Map // reflect.Type -> *responseTags

func responseTagsFor(t reflect.Type) *responseTags {
	if v, ok := responseTagCache.Load(t); ok {
		return v.(*responseTags) //nolint:forcetypeassert // only *responseTags is stored
	}

	tags := &responseTags{body: -1}

	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		if name := field.Tag.Get("header"); name != "" && name != "-" {
			tags.headers = append(tags.headers, headerField{index: i, name: name})
		}

		// The last body:"" field wins, as it always has.
		if bodyTag, ok := field.Tag.Lookup("body"); ok && bodyTag == "" {
			tags.body = i
		}
	}

	v, _ := responseTagCache.LoadOrStore(t, tags)

	return v.(*responseTags) //nolint:forcetypeassert // only *responseTags is stored
}

var jsonBufferPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// maxPooledBuffer keeps one huge response from pinning its buffer in the pool.
const maxPooledBuffer = 1 << 20

// writeJSONBody encodes body and writes it with status. The body is encoded
// before anything is written, so a value that cannot be encoded becomes a 500
// instead of a 200 with a truncated body. The output matches ctx.JSON: the
// same encoder settings and a trailing newline.
func writeJSONBody(ctx Context, status int, body any) {
	buf, _ := jsonBufferPool.Get().(*bytes.Buffer)
	if buf == nil {
		buf = new(bytes.Buffer)
	}

	buf.Reset()

	defer func() {
		if buf.Cap() <= maxPooledBuffer {
			jsonBufferPool.Put(buf)
		}
	}()

	if err := json.NewEncoder(buf).Encode(body); err != nil {
		handleError(ctx, InternalError(fmt.Errorf("failed to encode JSON response: %w", err)))

		return
	}

	w := ctx.Response()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

// cleanSensitiveFields returns v with every sensitive:"..." field cleaned. A
// value whose type holds nothing to clean is returned as is; anything else is
// copied, so v itself is never changed.
func cleanSensitiveFields(v any) any {
	if v == nil {
		return nil
	}

	return cleanSensitiveValue(reflect.ValueOf(v)).Interface()
}

func cleanSensitiveValue(rv reflect.Value) reflect.Value {
	if !rv.IsValid() || !typeNeedsCleaning(rv.Type()) {
		return rv
	}

	switch rv.Kind() {
	case reflect.Pointer:
		if rv.IsNil() {
			return rv
		}

		out := reflect.New(rv.Type().Elem())
		out.Elem().Set(cleanSensitiveValue(rv.Elem()))

		return out

	case reflect.Interface:
		if rv.IsNil() {
			return rv
		}

		return cleanSensitiveValue(rv.Elem())

	case reflect.Struct:
		return cleanSensitiveStruct(rv)

	case reflect.Slice:
		if rv.IsNil() {
			return rv
		}

		out := reflect.MakeSlice(rv.Type(), rv.Len(), rv.Len())
		for i := range rv.Len() {
			out.Index(i).Set(cleanSensitiveValue(rv.Index(i)))
		}

		return out

	case reflect.Array:
		out := reflect.New(rv.Type()).Elem()
		for i := range rv.Len() {
			out.Index(i).Set(cleanSensitiveValue(rv.Index(i)))
		}

		return out

	case reflect.Map:
		if rv.IsNil() {
			return rv
		}

		out := reflect.MakeMapWithSize(rv.Type(), rv.Len())

		iter := rv.MapRange()
		for iter.Next() {
			out.SetMapIndex(iter.Key(), cleanSensitiveValue(iter.Value()))
		}

		return out

	default:
		return rv
	}
}

// cleanSensitiveStruct copies rv, unexported fields included, then cleans the
// exported fields that are tagged sensitive or hold something that is.
func cleanSensitiveStruct(rv reflect.Value) reflect.Value {
	out := reflect.New(rv.Type()).Elem()
	out.Set(rv)

	for _, f := range sensitivePlanFor(rv.Type()).fields {
		if f.config != nil {
			out.Field(f.index).Set(applySensitiveCleaning(f.typ, f.config))
		} else {
			out.Field(f.index).Set(cleanSensitiveValue(rv.Field(f.index)))
		}
	}

	return out
}

// sensitivePlan lists the exported fields of a struct type that cleaning has
// to touch, with their parsed sensitive tags. Computed once per type.
type sensitivePlan struct {
	fields []sensitiveField
}

type sensitiveField struct {
	index  int
	typ    reflect.Type
	config *shared.SensitiveFieldConfig // nil: recurse into the field
}

var (
	sensitivePlanCache sync.Map // reflect.Type -> *sensitivePlan
	needsCleaningCache sync.Map // reflect.Type -> bool
)

func sensitivePlanFor(t reflect.Type) *sensitivePlan {
	if v, ok := sensitivePlanCache.Load(t); ok {
		return v.(*sensitivePlan) //nolint:forcetypeassert // only *sensitivePlan is stored
	}

	plan := &sensitivePlan{}

	for i := range t.NumField() {
		field := t.Field(i)
		if !field.IsExported() {
			continue
		}

		if config := shared.ParseSensitiveTag(field.Tag.Get("sensitive")); config != nil {
			plan.fields = append(plan.fields, sensitiveField{index: i, typ: field.Type, config: config})
		} else if typeNeedsCleaning(field.Type) {
			plan.fields = append(plan.fields, sensitiveField{index: i, typ: field.Type})
		}
	}

	v, _ := sensitivePlanCache.LoadOrStore(t, plan)

	return v.(*sensitivePlan) //nolint:forcetypeassert // only *sensitivePlan is stored
}

// typeNeedsCleaning reports whether a value of type t can hold a sensitive
// field. Values of other types are passed through without a copy.
func typeNeedsCleaning(t reflect.Type) bool {
	if v, ok := needsCleaningCache.Load(t); ok {
		return v.(bool) //nolint:forcetypeassert // only bool is stored
	}

	needs := computeNeedsCleaning(t, map[reflect.Type]bool{})
	needsCleaningCache.Store(t, needs)

	return needs
}

// computeNeedsCleaning walks t. A type met again while it is still being
// walked is a recursive type, and is assumed to need cleaning: that can only
// cost a copy, never skip a sensitive field.
func computeNeedsCleaning(t reflect.Type, walking map[reflect.Type]bool) bool {
	if v, ok := needsCleaningCache.Load(t); ok {
		return v.(bool) //nolint:forcetypeassert // only bool is stored
	}

	if walking[t] {
		return true
	}

	walking[t] = true
	defer delete(walking, t)

	switch t.Kind() {
	case reflect.Interface:
		// The dynamic value could be anything.
		return true
	case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
		return computeNeedsCleaning(t.Elem(), walking)
	case reflect.Struct:
		for field := range t.Fields() {
			if !field.IsExported() {
				continue
			}

			if shared.ParseSensitiveTag(field.Tag.Get("sensitive")) != nil {
				return true
			}

			if computeNeedsCleaning(field.Type, walking) {
				return true
			}
		}

		return false
	default:
		return false
	}
}

// applySensitiveCleaning returns the value a sensitive field of type t is
// replaced with.
func applySensitiveCleaning(t reflect.Type, config *shared.SensitiveFieldConfig) reflect.Value {
	switch config.Mode {
	case shared.SensitiveModeRedact:
		return replacementString(t, "[REDACTED]")
	case shared.SensitiveModeMask:
		return replacementString(t, config.Mask)
	default:
		return reflect.Zero(t)
	}
}

// replacementString returns s as a value of t when t is a string or a pointer
// to one, and the zero value of t otherwise.
func replacementString(t reflect.Type, s string) reflect.Value {
	switch {
	case t.Kind() == reflect.String:
		v := reflect.New(t).Elem()
		v.SetString(s)

		return v
	case t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.String:
		v := reflect.New(t.Elem())
		v.Elem().SetString(s)

		return v
	default:
		return reflect.Zero(t)
	}
}
