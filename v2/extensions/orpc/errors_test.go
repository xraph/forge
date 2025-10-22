package orpc

import "testing"

func TestGetErrorMessage(t *testing.T) {
	tests := []struct {
		code int
		want string
	}{
		{ErrParseError, "Parse error"},
		{ErrInvalidRequest, "Invalid Request"},
		{ErrMethodNotFound, "Method not found"},
		{ErrInvalidParams, "Invalid params"},
		{ErrInternalError, "Internal error"},
		{ErrServerError, "Server error"},
		{-32099, "Unknown error"},
	}
	
	for _, tt := range tests {
		got := GetErrorMessage(tt.code)
		if got != tt.want {
			t.Errorf("GetErrorMessage(%d) = %v, want %v", tt.code, got, tt.want)
		}
	}
}
