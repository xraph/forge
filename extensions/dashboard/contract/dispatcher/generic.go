package dispatcher

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/xraph/forge/extensions/dashboard/contract"
)

// RegisterQuery wraps a typed handler in a Handler-compatible closure that
// JSON-decodes Payload into I and encodes the returned O into Result.Data.
// I and O must be JSON-marshallable. Use struct{} for an empty-input intent.
func RegisterQuery[I, O any](d *Dispatcher, contributor, intent string, version int, fn func(ctx context.Context, in I, p contract.Principal) (O, error)) error {
	return d.Register(contributor, intent, version, wrapTyped[I, O](fn))
}

// RegisterCommand is identical in shape to RegisterQuery; both register a
// query/command handler. The dispatcher's wire layer enforces kind/capability
// matching against the manifest, so the only practical difference between the
// two helpers is intent of the caller — they're aliases.
func RegisterCommand[I, O any](d *Dispatcher, contributor, intent string, version int, fn func(ctx context.Context, in I, p contract.Principal) (O, error)) error {
	return d.Register(contributor, intent, version, wrapTyped[I, O](fn))
}

func wrapTyped[I, O any](fn func(ctx context.Context, in I, p contract.Principal) (O, error)) Handler {
	return func(ctx context.Context, payload json.RawMessage, params map[string]any, p contract.Principal) (*Result, error) {
		var in I
		// Decode params FIRST so payload (typically the user-authored
		// command body) overwrites any param-bound defaults on the same
		// field. resource.detail binds route placeholders into `params`
		// (e.g. `params: { id: { from: route.id } }`) — without this
		// merge, detail handlers expecting an ID-shaped Input always
		// receive the zero value and fail with "id is required".
		if len(params) > 0 {
			b, mErr := json.Marshal(params)
			if mErr != nil {
				return nil, &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("invalid params: %v", mErr)}
			}
			if err := json.Unmarshal(b, &in); err != nil {
				return nil, &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("invalid params: %v", err)}
			}
		}
		if len(payload) > 0 && string(payload) != "null" {
			if err := json.Unmarshal(payload, &in); err != nil {
				return nil, &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("invalid payload: %v", err)}
			}
		}
		out, err := fn(ctx, in, p)
		if err != nil {
			return nil, err
		}
		data, mErr := json.Marshal(out)
		if mErr != nil {
			return nil, &contract.Error{Code: contract.CodeInternal, Message: fmt.Sprintf("marshal output: %v", mErr)}
		}
		return &Result{Data: data}, nil
	}
}

// RegisterSubscription wraps a typed subscription handler. The pump goroutine
// JSON-encodes each typed E event into a contract.StreamEvent before
// forwarding into the broker's channel.
func RegisterSubscription[P, E any](d *Dispatcher, contributor, intent string, version int, fn func(ctx context.Context, in P, p contract.Principal) (<-chan E, func(), error)) error {
	wrapped := func(ctx context.Context, params map[string]any, principal contract.Principal) (<-chan contract.StreamEvent, func(), error) {
		var in P
		if len(params) > 0 {
			// Decode by remarshalling — slow but tolerable; subscription params are tiny.
			b, _ := json.Marshal(params)
			if err := json.Unmarshal(b, &in); err != nil {
				return nil, nil, &contract.Error{Code: contract.CodeBadRequest, Message: fmt.Sprintf("invalid params: %v", err)}
			}
		}
		typedCh, stop, err := fn(ctx, in, principal)
		if err != nil {
			return nil, nil, err
		}
		out := make(chan contract.StreamEvent, 4)
		var seq uint64
		go func() {
			defer close(out)
			for ev := range typedCh {
				seq++
				payload, mErr := json.Marshal(ev)
				if mErr != nil {
					// Drop the event if it can't be marshalled; log server-side.
					continue
				}
				select {
				case out <- contract.StreamEvent{Intent: intent, Payload: payload, Seq: seq}:
				case <-ctx.Done():
					return
				}
			}
		}()
		return out, stop, nil
	}
	return d.RegisterSubscription(contributor, intent, version, wrapped)
}
