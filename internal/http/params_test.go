package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCtx_ParamInt(t *testing.T) {
	tests := []struct {
		name      string
		paramName string
		paramVal  string
		want      int
		wantErr   bool
	}{
		{
			name:      "valid integer",
			paramName: "id",
			paramVal:  "123",
			want:      123,
			wantErr:   false,
		},
		{
			name:      "negative integer",
			paramName: "offset",
			paramVal:  "-10",
			want:      -10,
			wantErr:   false,
		},
		{
			name:      "invalid integer",
			paramName: "id",
			paramVal:  "abc",
			want:      0,
			wantErr:   true,
		},
		{
			name:      "missing parameter",
			paramName: "missing",
			paramVal:  "",
			want:      0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			ctx := NewContext(w, req, nil).(*Ctx)

			if tt.paramVal != "" {
				ctx.setParam(tt.paramName, tt.paramVal)
			}

			got, err := ctx.ParamInt(tt.paramName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParamInt() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("ParamInt() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCtx_ParamInt64(t *testing.T) {
	tests := []struct {
		name      string
		paramName string
		paramVal  string
		want      int64
		wantErr   bool
	}{
		{
			name:      "valid int64",
			paramName: "id",
			paramVal:  "9223372036854775807",
			want:      9223372036854775807,
			wantErr:   false,
		},
		{
			name:      "invalid int64",
			paramName: "id",
			paramVal:  "not_a_number",
			want:      0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			ctx := NewContext(w, req, nil).(*Ctx)

			if tt.paramVal != "" {
				ctx.setParam(tt.paramName, tt.paramVal)
			}

			got, err := ctx.ParamInt64(tt.paramName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParamInt64() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("ParamInt64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCtx_ParamFloat64(t *testing.T) {
	tests := []struct {
		name      string
		paramName string
		paramVal  string
		want      float64
		wantErr   bool
	}{
		{
			name:      "valid float",
			paramName: "price",
			paramVal:  "99.99",
			want:      99.99,
			wantErr:   false,
		},
		{
			name:      "scientific notation",
			paramName: "value",
			paramVal:  "1.23e10",
			want:      1.23e10,
			wantErr:   false,
		},
		{
			name:      "invalid float",
			paramName: "price",
			paramVal:  "not_a_float",
			want:      0,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			ctx := NewContext(w, req, nil).(*Ctx)

			if tt.paramVal != "" {
				ctx.setParam(tt.paramName, tt.paramVal)
			}

			got, err := ctx.ParamFloat64(tt.paramName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParamFloat64() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("ParamFloat64() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCtx_ParamBool(t *testing.T) {
	tests := []struct {
		name      string
		paramName string
		paramVal  string
		want      bool
		wantErr   bool
	}{
		{
			name:      "true value",
			paramName: "enabled",
			paramVal:  "true",
			want:      true,
			wantErr:   false,
		},
		{
			name:      "false value",
			paramName: "enabled",
			paramVal:  "false",
			want:      false,
			wantErr:   false,
		},
		{
			name:      "1 as true",
			paramName: "enabled",
			paramVal:  "1",
			want:      true,
			wantErr:   false,
		},
		{
			name:      "0 as false",
			paramName: "enabled",
			paramVal:  "0",
			want:      false,
			wantErr:   false,
		},
		{
			name:      "invalid bool",
			paramName: "enabled",
			paramVal:  "maybe",
			want:      false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			w := httptest.NewRecorder()
			ctx := NewContext(w, req, nil).(*Ctx)

			if tt.paramVal != "" {
				ctx.setParam(tt.paramName, tt.paramVal)
			}

			got, err := ctx.ParamBool(tt.paramName)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParamBool() error = %v, wantErr %v", err, tt.wantErr)

				return
			}

			if got != tt.want {
				t.Errorf("ParamBool() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCtx_ParamIntDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Test with valid value
	ctx.setParam("id", "42")

	if got := ctx.ParamIntDefault("id", 100); got != 42 {
		t.Errorf("ParamIntDefault() = %v, want %v", got, 42)
	}

	// Test with invalid value (should return default)
	ctx.setParam("invalid", "abc")

	if got := ctx.ParamIntDefault("invalid", 100); got != 100 {
		t.Errorf("ParamIntDefault() = %v, want %v", got, 100)
	}

	// Test with missing value (should return default)
	if got := ctx.ParamIntDefault("missing", 200); got != 200 {
		t.Errorf("ParamIntDefault() = %v, want %v", got, 200)
	}
}

func TestCtx_ParamInt64Default(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Test with valid value
	ctx.setParam("id", "123456789012345")

	if got := ctx.ParamInt64Default("id", 999); got != 123456789012345 {
		t.Errorf("ParamInt64Default() = %v, want %v", got, 123456789012345)
	}

	// Test with missing value (should return default)
	if got := ctx.ParamInt64Default("missing", 999); got != 999 {
		t.Errorf("ParamInt64Default() = %v, want %v", got, 999)
	}
}

func TestCtx_ParamFloat64Default(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Test with valid value
	ctx.setParam("price", "19.99")

	if got := ctx.ParamFloat64Default("price", 0.0); got != 19.99 {
		t.Errorf("ParamFloat64Default() = %v, want %v", got, 19.99)
	}

	// Test with missing value (should return default)
	if got := ctx.ParamFloat64Default("missing", 99.99); got != 99.99 {
		t.Errorf("ParamFloat64Default() = %v, want %v", got, 99.99)
	}
}

func TestCtx_ParamBoolDefault(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	ctx := NewContext(w, req, nil).(*Ctx)

	// Test with valid value
	ctx.setParam("enabled", "true")

	if got := ctx.ParamBoolDefault("enabled", false); got != true {
		t.Errorf("ParamBoolDefault() = %v, want %v", got, true)
	}

	// Test with missing value (should return default)
	if got := ctx.ParamBoolDefault("missing", true); got != true {
		t.Errorf("ParamBoolDefault() = %v, want %v", got, true)
	}
}
