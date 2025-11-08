package orpc

import (
	"testing"
)

func TestNewErrorResponse(t *testing.T) {
	resp := NewErrorResponse(1, ErrMethodNotFound, "Method not found")

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}

	if resp.Error == nil {
		t.Fatal("expected error to be set")
	}

	if resp.Error.Code != ErrMethodNotFound {
		t.Errorf("expected code %d, got %d", ErrMethodNotFound, resp.Error.Code)
	}

	if resp.ID != 1 {
		t.Errorf("expected id 1, got %v", resp.ID)
	}
}

func TestNewErrorResponseWithData(t *testing.T) {
	data := map[string]string{"detail": "extra info"}
	resp := NewErrorResponseWithData("req-123", ErrInvalidParams, "Invalid params", data)

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}

	if resp.Error == nil {
		t.Fatal("expected error to be set")
	}

	if resp.Error.Data == nil {
		t.Fatal("expected error data to be set")
	}

	if resp.ID != "req-123" {
		t.Errorf("expected id 'req-123', got %v", resp.ID)
	}
}

func TestNewSuccessResponse(t *testing.T) {
	result := map[string]string{"status": "ok"}
	resp := NewSuccessResponse(42, result)

	if resp.JSONRPC != "2.0" {
		t.Errorf("expected jsonrpc 2.0, got %s", resp.JSONRPC)
	}

	if resp.Result == nil {
		t.Fatal("expected result to be set")
	}

	if resp.Error != nil {
		t.Fatal("expected no error")
	}

	if resp.ID != 42 {
		t.Errorf("expected id 42, got %v", resp.ID)
	}
}

func TestIsNotification(t *testing.T) {
	tests := []struct {
		name string
		req  *Request
		want bool
	}{
		{
			name: "notification",
			req:  &Request{JSONRPC: "2.0", Method: "test", ID: nil},
			want: true,
		},
		{
			name: "request with id",
			req:  &Request{JSONRPC: "2.0", Method: "test", ID: 1},
			want: false,
		},
		{
			name: "request with string id",
			req:  &Request{JSONRPC: "2.0", Method: "test", ID: "abc"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.req.IsNotification(); got != tt.want {
				t.Errorf("IsNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestError_Error(t *testing.T) {
	tests := []struct {
		name string
		err  *Error
		want string
	}{
		{
			name: "error without data",
			err:  &Error{Code: -32601, Message: "Method not found"},
			want: "JSON-RPC error -32601: Method not found",
		},
		{
			name: "error with data",
			err:  &Error{Code: -32602, Message: "Invalid params", Data: "extra info"},
			want: "JSON-RPC error -32602: Invalid params (data: extra info)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %v, want %v", got, tt.want)
			}
		})
	}
}
