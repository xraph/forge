package auth

import "errors"

var (
	// ErrProviderNotFound is returned when a provider is not found
	ErrProviderNotFound = errors.New("auth provider not found")

	// ErrProviderExists is returned when a provider already exists
	ErrProviderExists = errors.New("auth provider already exists")

	// ErrInvalidConfiguration is returned when configuration is invalid
	ErrInvalidConfiguration = errors.New("invalid auth configuration")

	// ErrMissingCredentials is returned when credentials are missing
	ErrMissingCredentials = errors.New("missing authentication credentials")

	// ErrInvalidCredentials is returned when credentials are invalid
	ErrInvalidCredentials = errors.New("invalid credentials")

	// ErrInsufficientScopes is returned when required scopes are missing
	ErrInsufficientScopes = errors.New("insufficient scopes")

	// ErrAuthenticationFailed is returned when authentication fails
	ErrAuthenticationFailed = errors.New("authentication failed")

	// ErrAuthorizationFailed is returned when authorization fails
	ErrAuthorizationFailed = errors.New("authorization failed")

	// ErrTokenExpired is returned when a token has expired
	ErrTokenExpired = errors.New("token expired")

	// ErrTokenInvalid is returned when a token is invalid
	ErrTokenInvalid = errors.New("invalid token")
)
