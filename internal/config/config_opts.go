package config

import configcore "github.com/xraph/forge/internal/config/core"

// WithDefault sets a default value.
func WithDefault(value any) configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.Default = value
	}
}

// WithRequired marks the key as required.
func WithRequired() configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.Required = true
	}
}

// WithValidator adds a validation function.
func WithValidator(fn func(any) error) configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.Validator = fn
	}
}

// WithTransform adds a transformation function.
func WithTransform(fn func(any) any) configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.Transform = fn
	}
}

// WithOnMissing sets a callback for missing keys.
func WithOnMissing(fn func(string) any) configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.OnMissing = fn
	}
}

// AllowEmpty allows empty values.
func AllowEmpty() configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.AllowEmpty = true
	}
}

// WithCacheKey sets a custom cache key.
func WithCacheKey(key string) configcore.GetOption {
	return func(opts *configcore.GetOptions) {
		opts.CacheKey = key
	}
}
