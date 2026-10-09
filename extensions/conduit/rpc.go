package conduit

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/xraph/forge/extensions/conduit/core"
)

type RPCConfig = core.RPCConfig
type RPCError = core.RPCError
type RPCCode = core.RPCCode

const (
	RPCBadRequest       = core.RPCBadRequest
	RPCNotFound         = core.RPCNotFound
	RPCPermissionDenied = core.RPCPermissionDenied
	RPCConflict         = core.RPCConflict
	RPCUnavailable      = core.RPCUnavailable
	RPCDeadlineExceeded = core.RPCDeadlineExceeded
	RPCCanceled         = core.RPCCanceled
	RPCInternal         = core.RPCInternal
)

// ProcedureType names a versioned request and response contract.
type ProcedureType[Request, Response any] struct {
	Name             string
	ValidateRequest  func(Request) error
	ValidateResponse func(Response) error
}

// Procedure declares typed broker RPC independently of the transport.
func Procedure[Request, Response any](name string) ProcedureType[Request, Response] {
	return ProcedureType[Request, Response]{Name: name}
}

// Request carries typed input and caller correlation metadata.
type Request[T any] struct {
	Data     T
	Envelope Envelope
}

// Handle binds a typed procedure before Forge initializes the runtime.
func Handle[In, Out any](r *Runtime, procedure ProcedureType[In, Out], handler func(context.Context, Request[In]) (Out, error)) error {
	if handler == nil {
		return errors.New("conduit: RPC handler is required")
	}

	return r.BindRPC(procedure.Name, func(ctx context.Context, envelope Envelope) (json.RawMessage, error) {
		var input In
		if err := json.Unmarshal(envelope.Data, &input); err != nil {
			return nil, &RPCError{Code: RPCBadRequest, Message: "Invalid request payload"}
		}

		if procedure.ValidateRequest != nil {
			if err := procedure.ValidateRequest(input); err != nil {
				return nil, &RPCError{Code: RPCBadRequest, Message: "Request validation failed"}
			}
		}

		output, err := handler(ctx, Request[In]{Data: input, Envelope: envelope})
		if err != nil {
			return nil, err
		}

		if procedure.ValidateResponse != nil {
			if err := procedure.ValidateResponse(output); err != nil {
				return nil, err
			}
		}

		return json.Marshal(output)
	})
}

// Call sends one typed request by logical service name with no automatic retries.
func Call[In, Out any](ctx context.Context, r *Runtime, service string, procedure ProcedureType[In, Out], input In, options ...PublishOption) (Out, error) {
	var output Out

	data, err := json.Marshal(input)
	if err != nil {
		return output, err
	}

	envelope := Envelope{Data: data}

	for _, option := range options {
		if option == nil {
			return output, errors.New("conduit: nil RPC option")
		}

		option(&envelope)
	}

	response, err := r.CallRPC(ctx, service, procedure.Name, envelope, func(message Envelope) error {
		var validated In
		if err := json.Unmarshal(message.Data, &validated); err != nil {
			return err
		}

		if procedure.ValidateRequest != nil {
			return procedure.ValidateRequest(validated)
		}

		return nil
	})
	if err != nil {
		return output, err
	}

	if err := json.Unmarshal(response.Data, &output); err != nil {
		return output, &RPCError{Code: RPCInternal, Message: "Invalid response payload"}
	}

	if procedure.ValidateResponse != nil {
		if err := procedure.ValidateResponse(output); err != nil {
			return output, &RPCError{Code: RPCInternal, Message: "Response validation failed"}
		}
	}

	return output, nil
}
