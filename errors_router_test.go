package forge

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/xraph/go-utils/errs"
)

func TestHTTPError_Error(t *testing.T) {
	err := NewHTTPError(404, "not found")
	assert.Equal(t, "not found", err.Error())
}

func TestHTTPError_Error_NoMessage(t *testing.T) {
	err := &HTTPError{HttpStatusCode: 500}
	assert.Equal(t, http.StatusText(http.StatusInternalServerError), err.Error())
}

func TestHTTPError_Error_WithErr(t *testing.T) {
	err := errs.NewHTTPError(500, "inner error")
	assert.Equal(t, "inner error", err.Error())
}

func TestBadRequest(t *testing.T) {
	err := BadRequest("bad request")
	assert.Equal(t, http.StatusBadRequest, err.StatusCode())
	// assert.Equal(t, "bad request", err.Message)
}

func TestUnauthorized(t *testing.T) {
	err := Unauthorized("unauthorized")
	assert.Equal(t, http.StatusUnauthorized, err.StatusCode())
	// assert.Equal(t, "unauthorized", err.Message)
}

func TestForbidden(t *testing.T) {
	err := Forbidden("forbidden")
	assert.Equal(t, http.StatusForbidden, err.StatusCode())
	// assert.Equal(t, "forbidden", err.Message)
}

func TestNotFound(t *testing.T) {
	err := NotFound("not found")
	assert.Equal(t, http.StatusNotFound, err.StatusCode())
	// assert.Equal(t, "not found", err.Message)
}

func TestInternalError(t *testing.T) {
	innerErr := errors.New("internal")
	err := InternalError(innerErr)
	assert.Equal(t, http.StatusInternalServerError, err.StatusCode())
	// assert.Equal(t, innerErr, err.Err)
}
