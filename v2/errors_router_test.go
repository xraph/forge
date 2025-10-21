package forge

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestHTTPError_Error(t *testing.T) {
	err := NewHTTPError(404, "not found")
	assert.Equal(t, "not found", err.Error())
}

func TestHTTPError_Error_NoMessage(t *testing.T) {
	err := &HTTPError{Code: 500}
	assert.Equal(t, http.StatusText(500), err.Error())
}

func TestHTTPError_Error_WithErr(t *testing.T) {
	innerErr := errors.New("inner error")
	err := &HTTPError{Code: 500, Err: innerErr}
	assert.Equal(t, "inner error", err.Error())
}

func TestHTTPError_Unwrap(t *testing.T) {
	innerErr := errors.New("inner error")
	err := &HTTPError{Code: 500, Err: innerErr}
	
	assert.Equal(t, innerErr, err.Unwrap())
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("bad request")
	assert.Equal(t, http.StatusBadRequest, err.Code)
	assert.Equal(t, "bad request", err.Message)
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("unauthorized")
	assert.Equal(t, http.StatusUnauthorized, err.Code)
	assert.Equal(t, "unauthorized", err.Message)
}

func TestForbidden(t *testing.T) {
	err := Forbidden("forbidden")
	assert.Equal(t, http.StatusForbidden, err.Code)
	assert.Equal(t, "forbidden", err.Message)
}

func TestNotFound(t *testing.T) {
	err := NotFound("not found")
	assert.Equal(t, http.StatusNotFound, err.Code)
	assert.Equal(t, "not found", err.Message)
}

func TestInternalError(t *testing.T) {
	innerErr := errors.New("internal")
	err := InternalError(innerErr)
	assert.Equal(t, http.StatusInternalServerError, err.Code)
	assert.Equal(t, innerErr, err.Err)
}

