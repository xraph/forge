package router

import (
	"time"

	"github.com/xraph/forge/internal/di"
)

// Route option implementations.
type nameOpt struct{ name string }

func (o *nameOpt) Apply(cfg *RouteConfig) {
	cfg.Name = o.name
}

type summaryOpt struct{ summary string }

func (o *summaryOpt) Apply(cfg *RouteConfig) {
	cfg.Summary = o.summary
}

type descriptionOpt struct{ desc string }

func (o *descriptionOpt) Apply(cfg *RouteConfig) {
	cfg.Description = o.desc
}

type tagsOpt struct{ tags []string }

func (o *tagsOpt) Apply(cfg *RouteConfig) {
	cfg.Tags = append(cfg.Tags, o.tags...)
}

type middlewareOpt struct{ middleware []Middleware }

func (o *middlewareOpt) Apply(cfg *RouteConfig) {
	cfg.Middleware = append(cfg.Middleware, o.middleware...)
}

type timeoutOpt struct{ timeout time.Duration }

func (o *timeoutOpt) Apply(cfg *RouteConfig) {
	cfg.Timeout = o.timeout
}

type metadataOpt struct {
	key   string
	value any
}

func (o *metadataOpt) Apply(cfg *RouteConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata[o.key] = o.value
}

type extensionOpt struct {
	name string
	ext  Extension
}

func (o *extensionOpt) Apply(cfg *RouteConfig) {
	if cfg.Extensions == nil {
		cfg.Extensions = make(map[string]Extension)
	}

	cfg.Extensions[o.name] = o.ext
}

type operationIDOpt struct{ id string }

func (o *operationIDOpt) Apply(cfg *RouteConfig) {
	cfg.OperationID = o.id
}

type deprecatedOpt struct{}

func (o *deprecatedOpt) Apply(cfg *RouteConfig) {
	cfg.Deprecated = true
}

// Group option implementations.
type groupMiddlewareOpt struct{ middleware []Middleware }

func (o *groupMiddlewareOpt) Apply(cfg *GroupConfig) {
	cfg.Middleware = append(cfg.Middleware, o.middleware...)
}

type groupTagsOpt struct{ tags []string }

func (o *groupTagsOpt) Apply(cfg *GroupConfig) {
	cfg.Tags = append(cfg.Tags, o.tags...)
}

type groupMetadataOpt struct {
	key   string
	value any
}

func (o *groupMetadataOpt) Apply(cfg *GroupConfig) {
	if cfg.Metadata == nil {
		cfg.Metadata = make(map[string]any)
	}

	cfg.Metadata[o.key] = o.value
}

// Router option implementations.
type adapterOpt struct{ adapter RouterAdapter }

func (o *adapterOpt) Apply(cfg *routerConfig) {
	cfg.adapter = o.adapter
}

type containerOpt struct{ container di.Container }

func (o *containerOpt) Apply(cfg *routerConfig) {
	cfg.container = o.container
}

type loggerOpt struct{ logger Logger }

func (o *loggerOpt) Apply(cfg *routerConfig) {
	cfg.logger = o.logger
}

type errorHandlerOpt struct{ handler ErrorHandler }

func (o *errorHandlerOpt) Apply(cfg *routerConfig) {
	cfg.errorHandler = o.handler
}

type recoveryOpt struct{}

func (o *recoveryOpt) Apply(cfg *routerConfig) {
	cfg.recovery = true
}
