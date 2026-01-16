package router

import (
	"errors"
	"fmt"
	"net/http"
	"reflect"

	"github.com/xraph/forge/internal/shared"
	forge_http "github.com/xraph/go-utils/http"
	"github.com/xraph/vessel"
)

// HandlerPattern indicates the handler signature.
type HandlerPattern int

const (
	PatternStandard    HandlerPattern = iota // func(w, r)
	PatternContext                           // func(ctx) error
	PatternOpinionated                       // func(ctx, req) (resp, error)
	PatternService                           // func(ctx, svc) error
	PatternCombined                          // func(ctx, svc, req) (resp, error)
)

// handlerInfo contains analyzed handler information.
type handlerInfo struct {
	pattern      HandlerPattern
	funcValue    reflect.Value
	funcType     reflect.Type
	requestType  reflect.Type
	responseType reflect.Type
	serviceType  reflect.Type
	serviceName  string
}

// detectHandlerPattern analyzes handler signature.
func detectHandlerPattern(handler any) (*handlerInfo, error) {
	if handler == nil {
		return nil, errors.New("handler is nil")
	}

	// Check if already http.Handler or http.HandlerFunc
	if _, ok := handler.(http.Handler); ok {
		return &handlerInfo{pattern: PatternStandard}, nil
	}

	if _, ok := handler.(http.HandlerFunc); ok {
		return &handlerInfo{pattern: PatternStandard}, nil
	}

	funcValue := reflect.ValueOf(handler)
	funcType := funcValue.Type()

	if funcType.Kind() != reflect.Func {
		return nil, fmt.Errorf("handler must be a function, got %v", funcType.Kind())
	}

	numIn := funcType.NumIn()
	numOut := funcType.NumOut()

	// Pattern 1: func(w http.ResponseWriter, r *http.Request)
	if numIn == 2 && numOut == 0 {
		if isResponseWriter(funcType.In(0)) && isRequest(funcType.In(1)) {
			return &handlerInfo{
				pattern:   PatternStandard,
				funcValue: funcValue,
				funcType:  funcType,
			}, nil
		}
	}

	// All other patterns start with Context
	if numIn == 0 || !isContext(funcType.In(0)) {
		return nil, errors.New("handler must start with forge.Context parameter")
	}

	// Pattern 2: func(ctx Context) error
	if numIn == 1 && numOut == 1 && isError(funcType.Out(0)) {
		return &handlerInfo{
			pattern:   PatternContext,
			funcValue: funcValue,
			funcType:  funcType,
		}, nil
	}

	// Pattern 3: func(ctx Context, req *Request) (*Response, error)
	if numIn == 2 && numOut == 2 {
		if isPointer(funcType.In(1)) && isPointer(funcType.Out(0)) && isError(funcType.Out(1)) {
			return &handlerInfo{
				pattern:      PatternOpinionated,
				funcValue:    funcValue,
				funcType:     funcType,
				requestType:  funcType.In(1).Elem(),
				responseType: funcType.Out(0).Elem(),
			}, nil
		}
	}

	// Pattern 4: func(ctx Context, svc Service) error
	if numIn == 2 && numOut == 1 && isError(funcType.Out(0)) {
		serviceType := funcType.In(1)
		serviceName := getServiceName(serviceType)

		return &handlerInfo{
			pattern:     PatternService,
			funcValue:   funcValue,
			funcType:    funcType,
			serviceType: serviceType,
			serviceName: serviceName,
		}, nil
	}

	// Pattern 5: func(ctx Context, svc Service, req *Request) (*Response, error)
	if numIn == 3 && numOut == 2 {
		if isPointer(funcType.In(2)) && isPointer(funcType.Out(0)) && isError(funcType.Out(1)) {
			serviceType := funcType.In(1)
			serviceName := getServiceName(serviceType)

			return &handlerInfo{
				pattern:      PatternCombined,
				funcValue:    funcValue,
				funcType:     funcType,
				serviceType:  serviceType,
				serviceName:  serviceName,
				requestType:  funcType.In(2).Elem(),
				responseType: funcType.Out(0).Elem(),
			}, nil
		}
	}

	return nil, fmt.Errorf("unsupported handler signature: %v", funcType)
}

// convertHandler converts any handler to http.Handler.
func convertHandler(handler any, container vessel.Vessel, errorHandler ErrorHandler) (http.Handler, error) {
	// Fast path for existing http.Handler
	if h, ok := handler.(http.Handler); ok {
		return h, nil
	}

	if h, ok := handler.(http.HandlerFunc); ok {
		return h, nil
	}

	info, err := detectHandlerPattern(handler)
	if err != nil {
		return nil, err
	}

	switch info.pattern {
	case PatternStandard:
		return convertStandardHandler(info), nil
	case PatternContext:
		return convertContextHandler(info, container, errorHandler), nil
	case PatternOpinionated:
		return convertOpinionatedHandler(info, container, errorHandler), nil
	case PatternService:
		return convertServiceHandler(info, container, errorHandler), nil
	case PatternCombined:
		return convertCombinedHandler(info, container, errorHandler), nil
	default:
		return nil, fmt.Errorf("unknown handler pattern: %v", info.pattern)
	}
}

// convertStandardHandler converts func(w, r) to http.Handler.
func convertStandardHandler(info *handlerInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info.funcValue.Call([]reflect.Value{
			reflect.ValueOf(w),
			reflect.ValueOf(r),
		})
	})
}

// convertContextHandler converts func(ctx) error to http.Handler.
func convertContextHandler(info *handlerInfo, container vessel.Vessel, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		results := info.funcValue.Call([]reflect.Value{reflect.ValueOf(ctx)})

		if len(results) > 0 && !results[0].IsNil() {
			err := results[0].Interface().(error)
			if errorHandler != nil {
				_ = errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}
		}
	})
}

// convertOpinionatedHandler converts func(ctx, req) (resp, error) to http.Handler.
func convertOpinionatedHandler(info *handlerInfo, container vessel.Vessel, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Create request instance
		req := reflect.New(info.requestType)

		// Bind request from all sources (path, query, header, body)
		// BindRequest handles detection of body fields internally
		if err := ctx.BindRequest(req.Interface()); err != nil {
			handleError(ctx, fmt.Errorf("invalid request: %w", err))

			return
		}

		// Call handler
		results := info.funcValue.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			req,
		})

		// Handle response
		resp := results[0]
		errVal := results[1]

		if !errVal.IsNil() {
			err := errVal.Interface().(error)
			if errorHandler != nil {
				_ = errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}

			return
		}

		if !resp.IsNil() {
			body := processResponse(ctx, resp)
			if body != nil {
				_ = ctx.JSON(http.StatusOK, body)
			}
		}
	})
}

// convertServiceHandler converts func(ctx, svc) error to http.Handler.
func convertServiceHandler(info *handlerInfo, container vessel.Vessel, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Resolve service
		service, err := resolveService(ctx, info.serviceType, info.serviceName)
		if err != nil {
			handleError(ctx, InternalError(fmt.Errorf("failed to resolve service: %w", err)))

			return
		}

		// Call handler
		results := info.funcValue.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			service,
		})

		if len(results) > 0 && !results[0].IsNil() {
			err := results[0].Interface().(error)
			if errorHandler != nil {
				_ = errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}
		}
	})
}

// convertCombinedHandler converts func(ctx, svc, req) (resp, error) to http.Handler.
func convertCombinedHandler(info *handlerInfo, container vessel.Vessel, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := forge_http.NewContext(w, r, container)
		defer ctx.(forge_http.ContextWithClean).Cleanup()

		// Resolve service
		service, err := resolveService(ctx, info.serviceType, info.serviceName)
		if err != nil {
			handleError(ctx, InternalError(fmt.Errorf("failed to resolve service: %w", err)))

			return
		}

		// Create request instance
		req := reflect.New(info.requestType)

		// Bind request from all sources (path, query, header, body)
		// BindRequest handles detection of body fields internally
		if err := ctx.BindRequest(req.Interface()); err != nil {
			handleError(ctx, BadRequest(fmt.Sprintf("invalid request: %v", err)))

			return
		}

		// Call handler
		results := info.funcValue.Call([]reflect.Value{
			reflect.ValueOf(ctx),
			service,
			req,
		})

		// Handle response
		resp := results[0]
		errVal := results[1]

		if !errVal.IsNil() {
			err := errVal.Interface().(error)
			if errorHandler != nil {
				_ = errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}

			return
		}

		if !resp.IsNil() {
			body := processResponse(ctx, resp)
			if body != nil {
				_ = ctx.JSON(http.StatusOK, body)
			}
		}
	})
}

// Helper functions for type checking.
func isResponseWriter(t reflect.Type) bool {
	return t.Implements(reflect.TypeFor[http.ResponseWriter]())
}

func isRequest(t reflect.Type) bool {
	return t == reflect.TypeFor[*http.Request]()
}

func isContext(t reflect.Type) bool {
	return t.Implements(reflect.TypeFor[Context]())
}

func isError(t reflect.Type) bool {
	return t.Implements(reflect.TypeFor[error]())
}

func isPointer(t reflect.Type) bool {
	return t.Kind() == reflect.Ptr
}

func getServiceName(t reflect.Type) string {
	// Remove pointer if present
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// Use full package path + type name
	if t.PkgPath() != "" {
		return t.PkgPath() + "." + t.Name()
	}

	return t.Name()
}

func resolveService(ctx Context, serviceType reflect.Type, serviceName string) (reflect.Value, error) {
	// Try to resolve by name
	instance, err := ctx.Resolve(serviceName)
	if err != nil {
		return reflect.Value{}, err
	}

	val := reflect.ValueOf(instance)

	// Check type compatibility
	if !val.Type().AssignableTo(serviceType) {
		return reflect.Value{}, fmt.Errorf("service %s has type %v, expected %v", serviceName, val.Type(), serviceType)
	}

	return val, nil
}

func handleError(ctx Context, err error) {
	httpErr := &HTTPError{}
	if errors.As(err, &httpErr) {
		_ = ctx.JSON(httpErr.HttpStatusCode, httpErr.ResponseBody())
	} else {
		_ = ctx.JSON(http.StatusInternalServerError, map[string]any{
			"code":    "INTERNAL_SERVER_ERROR",
			"message": err.Error(),
		})
	}
}

// processResponse handles response struct tags using shared logic.
// - Sets header:"..." fields as HTTP response headers
// - Unwraps body:"" fields to return just the body content
// - Falls back to full struct serialization if no special tags found.
func processResponse(ctx Context, resp reflect.Value) any {
	if resp.Kind() == reflect.Ptr && resp.IsNil() {
		return nil
	}

	return shared.ProcessResponseValue(resp.Interface(), ctx.SetHeader)
}
