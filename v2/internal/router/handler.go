package router

import (
	"fmt"
	"net/http"
	"reflect"

	"github.com/xraph/forge/v2/internal/di"
)

// HandlerPattern indicates the handler signature
type HandlerPattern int

const (
	PatternStandard    HandlerPattern = iota // func(w, r)
	PatternContext                           // func(ctx) error
	PatternOpinionated                       // func(ctx, req) (resp, error)
	PatternService                           // func(ctx, svc) error
	PatternCombined                          // func(ctx, svc, req) (resp, error)
)

// handlerInfo contains analyzed handler information
type handlerInfo struct {
	pattern      HandlerPattern
	funcValue    reflect.Value
	funcType     reflect.Type
	requestType  reflect.Type
	responseType reflect.Type
	serviceType  reflect.Type
	serviceName  string
}

// detectHandlerPattern analyzes handler signature
func detectHandlerPattern(handler any) (*handlerInfo, error) {
	if handler == nil {
		return nil, fmt.Errorf("handler is nil")
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
		return nil, fmt.Errorf("handler must start with forge.Context parameter")
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

// convertHandler converts any handler to http.Handler
func convertHandler(handler any, container di.Container, errorHandler ErrorHandler) (http.Handler, error) {
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

// convertStandardHandler converts func(w, r) to http.Handler
func convertStandardHandler(info *handlerInfo) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info.funcValue.Call([]reflect.Value{
			reflect.ValueOf(w),
			reflect.ValueOf(r),
		})
	})
}

// convertContextHandler converts func(ctx) error to http.Handler
func convertContextHandler(info *handlerInfo, container di.Container, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, container)
		defer ctx.(di.ContextWithClean).Cleanup()

		results := info.funcValue.Call([]reflect.Value{reflect.ValueOf(ctx)})

		if len(results) > 0 && !results[0].IsNil() {
			err := results[0].Interface().(error)
			if errorHandler != nil {
				errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}
		}
	})
}

// shouldBindRequestBody determines if we should try to bind a request body
// based on HTTP method and struct field tags
func shouldBindRequestBody(method string, requestType reflect.Type) bool {
	// Methods that typically don't have a body
	noBodyMethods := map[string]bool{
		"GET":     true,
		"HEAD":    true,
		"DELETE":  true,
		"OPTIONS": true,
	}

	// Check if struct has any body fields (json or body tags)
	hasBodyFields := false
	for i := 0; i < requestType.NumField(); i++ {
		field := requestType.Field(i)

		// Skip unexported fields
		if !field.IsExported() {
			continue
		}

		// Check for body or json tags (but not path, query, or header tags)
		if field.Tag.Get("path") == "" &&
			field.Tag.Get("query") == "" &&
			field.Tag.Get("header") == "" {
			// Has json or body tag, or no special tags (default to body)
			if field.Tag.Get("json") != "" && field.Tag.Get("json") != "-" {
				hasBodyFields = true
				break
			}
			if field.Tag.Get("body") != "" && field.Tag.Get("body") != "-" {
				hasBodyFields = true
				break
			}
		}
	}

	// If method typically has no body and struct has no body fields, skip binding
	if noBodyMethods[method] && !hasBodyFields {
		return false
	}

	// If struct has no body fields at all (only path/query/header), skip binding
	if !hasBodyFields {
		return false
	}

	// Otherwise, try to bind
	return true
}

// convertOpinionatedHandler converts func(ctx, req) (resp, error) to http.Handler
func convertOpinionatedHandler(info *handlerInfo, container di.Container, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, container)
		defer ctx.(di.ContextWithClean).Cleanup()

		// Create request instance
		req := reflect.New(info.requestType)

		// Only bind request body if the struct has body fields or if method expects a body
		// For unified schemas, check if struct has json/body tagged fields
		// For GET/DELETE/HEAD requests without body fields, skip binding
		shouldBind := shouldBindRequestBody(r.Method, info.requestType)

		if shouldBind {
			// Bind request body
			if err := ctx.Bind(req.Interface()); err != nil {
				handleError(ctx, BadRequest(fmt.Sprintf("invalid request: %v", err)))
				return
			}
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
				errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}
			return
		}

		if !resp.IsNil() {
			_ = ctx.JSON(http.StatusOK, resp.Interface())
		}
	})
}

// convertServiceHandler converts func(ctx, svc) error to http.Handler
func convertServiceHandler(info *handlerInfo, container di.Container, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, container)
		defer ctx.(di.ContextWithClean).Cleanup()

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
				errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}
		}
	})
}

// convertCombinedHandler converts func(ctx, svc, req) (resp, error) to http.Handler
func convertCombinedHandler(info *handlerInfo, container di.Container, errorHandler ErrorHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := di.NewContext(w, r, container)
		defer ctx.(di.ContextWithClean).Cleanup()

		// Resolve service
		service, err := resolveService(ctx, info.serviceType, info.serviceName)
		if err != nil {
			handleError(ctx, InternalError(fmt.Errorf("failed to resolve service: %w", err)))
			return
		}

		// Create request instance
		req := reflect.New(info.requestType)

		// Only bind request body if the struct has body fields
		shouldBind := shouldBindRequestBody(r.Method, info.requestType)

		if shouldBind {
			// Bind request body
			if err := ctx.Bind(req.Interface()); err != nil {
				handleError(ctx, BadRequest(fmt.Sprintf("invalid request: %v", err)))
				return
			}
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
				errorHandler.HandleError(ctx.Context(), err)
			} else {
				handleError(ctx, err)
			}
			return
		}

		if !resp.IsNil() {
			_ = ctx.JSON(http.StatusOK, resp.Interface())
		}
	})
}

// Helper functions for type checking
func isResponseWriter(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*http.ResponseWriter)(nil)).Elem())
}

func isRequest(t reflect.Type) bool {
	return t == reflect.TypeOf((*http.Request)(nil))
}

func isContext(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*Context)(nil)).Elem())
}

func isError(t reflect.Type) bool {
	return t.Implements(reflect.TypeOf((*error)(nil)).Elem())
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
	if httpErr, ok := err.(*HTTPError); ok {
		_ = ctx.JSON(httpErr.Code, map[string]string{
			"error": httpErr.Error(),
		})
	} else {
		_ = ctx.JSON(http.StatusInternalServerError, map[string]string{
			"error": err.Error(),
		})
	}
}
