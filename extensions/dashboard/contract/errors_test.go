package contract

import (
	"errors"
	"testing"
)

func TestError_CodeAndMessage(t *testing.T) {
	e := &Error{Code: CodePermissionDenied, Message: "no", CorrelationID: "c1"}
	if e.Code != "PERMISSION_DENIED" {
		t.Errorf("Code = %q, want PERMISSION_DENIED", e.Code)
	}
	if got := e.Error(); got != "PERMISSION_DENIED: no" {
		t.Errorf("Error() = %q", got)
	}
}

func TestError_Is(t *testing.T) {
	e := &Error{Code: CodeNotFound}
	if !errors.Is(e, ErrNotFound) {
		t.Error("errors.Is should match canonical sentinel")
	}
}

func TestCanonicalCodes_AllPresent(t *testing.T) {
	want := []ErrorCode{
		CodeBadRequest, CodeUnauthenticated, CodePermissionDenied,
		CodeNotFound, CodeConflict, CodeRateLimited,
		CodeUnsupportedVersion, CodeUnavailable, CodeInternal,
	}
	for _, c := range want {
		if c == "" {
			t.Errorf("canonical code missing")
		}
	}
}
