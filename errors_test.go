package forge

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceError_Error(t *testing.T) {
	innerErr := errors.New("inner error")
	serviceErr := NewServiceError("test-service", "start", innerErr)

	expected := "service test-service: start: inner error"
	assert.Equal(t, expected, serviceErr.Error())
}

func TestServiceError_Unwrap(t *testing.T) {
	innerErr := errors.New("inner error")
	serviceErr := NewServiceError("test-service", "resolve", innerErr)

	unwrapped := serviceErr.Unwrap()
	assert.Equal(t, innerErr, unwrapped)
}

func TestServiceError_ErrorsAs(t *testing.T) {
	innerErr := errors.New("inner error")
	serviceErr := NewServiceError("test-service", "stop", innerErr)

	var svcErr *ServiceError
	assert.True(t, errors.As(serviceErr, &svcErr))
	assert.Equal(t, "test-service", svcErr.Service)
	assert.Equal(t, "stop", svcErr.Operation)
	assert.Equal(t, innerErr, svcErr.Err)
}

func TestServiceError_ErrorsIs(t *testing.T) {
	innerErr := errors.New("inner error")
	serviceErr := NewServiceError("test-service", "health", innerErr)

	assert.True(t, errors.Is(serviceErr, innerErr))
}

func TestStandardErrors(t *testing.T) {
	// Test error constructors exist
	assert.NotNil(t, ErrServiceNotFound)
	assert.NotNil(t, ErrCircularDependency)
	assert.NotNil(t, ErrContainerStarted)
	assert.NotNil(t, ErrScopeEnded)

	// Test error constructor outputs
	assert.Contains(t, ErrServiceNotFound("test").Error(), "not found")
	assert.Contains(t, ErrCircularDependency([]string{"a", "b"}).Error(), "circular")
	assert.Contains(t, ErrContainerStarted.Error(), "already started")
	assert.Contains(t, ErrScopeEnded.Error(), "already ended")
}
