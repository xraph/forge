package config

import (
	"github.com/xraph/forge/pkg/common"
)

// GetOption defines functional options for advanced get operations
type GetOption = common.GetOption

// GetOptions contains options for advanced get operations
type GetOptions = common.GetOptions

// WithDefault sets a default value
func WithDefault(value interface{}) GetOption {
	return func(opts *GetOptions) {
		opts.Default = value
	}
}

// WithRequired marks the key as required
func WithRequired() GetOption {
	return func(opts *GetOptions) {
		opts.Required = true
	}
}

// WithValidator adds a validation function
func WithValidator(fn func(interface{}) error) GetOption {
	return func(opts *GetOptions) {
		opts.Validator = fn
	}
}

// WithTransform adds a transformation function
func WithTransform(fn func(interface{}) interface{}) GetOption {
	return func(opts *GetOptions) {
		opts.Transform = fn
	}
}

// WithOnMissing sets a callback for missing keys
func WithOnMissing(fn func(string) interface{}) GetOption {
	return func(opts *GetOptions) {
		opts.OnMissing = fn
	}
}

// AllowEmpty allows empty values
func AllowEmpty() GetOption {
	return func(opts *GetOptions) {
		opts.AllowEmpty = true
	}
}

// WithCacheKey sets a custom cache key
func WithCacheKey(key string) GetOption {
	return func(opts *GetOptions) {
		opts.CacheKey = key
	}
}
