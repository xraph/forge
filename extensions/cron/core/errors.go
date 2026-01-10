package core

import "errors"

// Standard errors for the cron extension.
var (
	ErrJobNotFound       = errors.New("job not found")
	ErrJobAlreadyExists  = errors.New("job already exists")
	ErrInvalidSchedule   = errors.New("invalid cron schedule")
	ErrHandlerNotFound   = errors.New("handler not found")
	ErrExecutionNotFound = errors.New("execution not found")
	ErrInvalidData       = errors.New("invalid data received from storage")
)
