package forge

// HandlerPattern indicates the handler signature
type HandlerPattern int

const (
	PatternStandard    HandlerPattern = iota // func(w, r)
	PatternContext                           // func(ctx) error
	PatternOpinionated                       // func(ctx, req) (resp, error)
	PatternService                           // func(ctx, svc) error
	PatternCombined                          // func(ctx, svc, req) (resp, error)
)
