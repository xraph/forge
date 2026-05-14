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
	codes := []ErrorCode{
		CodeBadRequest, CodeUnauthenticated, CodePermissionDenied,
		CodeNotFound, CodeConflict, CodeRateLimited,
		CodeUnsupportedVersion, CodeUnavailable, CodeInternal,
	}
	if len(codes) != 9 {
		t.Errorf("expected 9 canonical codes, got %d", len(codes))
	}
	seen := map[ErrorCode]bool{}
	for _, c := range codes {
		if c == "" {
			t.Errorf("canonical code is empty")
		}
		if seen[c] {
			t.Errorf("canonical code %q duplicated", c)
		}
		seen[c] = true
	}
	sentinels := []*Error{
		ErrBadRequest, ErrUnauthenticated, ErrPermissionDenied,
		ErrNotFound, ErrConflict, ErrRateLimited,
		ErrUnsupportedVersion, ErrUnavailable, ErrInternal,
	}
	for _, e := range sentinels {
		if e.Code == "" {
			t.Errorf("sentinel has empty code")
		}
	}
}
