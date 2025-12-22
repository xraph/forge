package shared

import (
	"testing"
)

func TestParseSensitiveTag(t *testing.T) {
	tests := []struct {
		name     string
		tagValue string
		want     *SensitiveFieldConfig
	}{
		{
			name:     "empty tag returns nil",
			tagValue: "",
			want:     nil,
		},
		{
			name:     "true value returns zero mode",
			tagValue: "true",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeZero},
		},
		{
			name:     "1 value returns zero mode",
			tagValue: "1",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeZero},
		},
		{
			name:     "redact value returns redact mode",
			tagValue: "redact",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeRedact},
		},
		{
			name:     "mask with value returns mask mode",
			tagValue: "mask:***",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeMask, Mask: "***"},
		},
		{
			name:     "mask with custom value",
			tagValue: "mask:[HIDDEN]",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeMask, Mask: "[HIDDEN]"},
		},
		{
			name:     "mask with empty value",
			tagValue: "mask:",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeMask, Mask: ""},
		},
		{
			name:     "unknown value defaults to zero mode",
			tagValue: "yes",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeZero},
		},
		{
			name:     "whitespace trimmed",
			tagValue: "  true  ",
			want:     &SensitiveFieldConfig{Mode: SensitiveModeZero},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSensitiveTag(tt.tagValue)
			if tt.want == nil {
				if got != nil {
					t.Errorf("ParseSensitiveTag() = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Errorf("ParseSensitiveTag() = nil, want %v", tt.want)
				return
			}
			if got.Mode != tt.want.Mode {
				t.Errorf("ParseSensitiveTag().Mode = %v, want %v", got.Mode, tt.want.Mode)
			}
			if got.Mask != tt.want.Mask {
				t.Errorf("ParseSensitiveTag().Mask = %q, want %q", got.Mask, tt.want.Mask)
			}
		})
	}
}

func TestCleanSensitiveFields_BasicStruct(t *testing.T) {
	type User struct {
		ID       string `json:"id"`
		Email    string `json:"email"`
		Password string `json:"password" sensitive:"true"`
		APIKey   string `json:"api_key" sensitive:"redact"`
		Token    string `json:"token" sensitive:"mask:***"`
	}

	input := &User{
		ID:       "123",
		Email:    "test@example.com",
		Password: "secret123",
		APIKey:   "sk-1234567890",
		Token:    "jwt-token-here",
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.(*User)
	if !ok {
		t.Fatalf("Expected *User, got %T", result)
	}

	// Non-sensitive fields should be unchanged
	if cleaned.ID != "123" {
		t.Errorf("ID = %q, want %q", cleaned.ID, "123")
	}
	if cleaned.Email != "test@example.com" {
		t.Errorf("Email = %q, want %q", cleaned.Email, "test@example.com")
	}

	// sensitive:"true" should be zero value
	if cleaned.Password != "" {
		t.Errorf("Password = %q, want empty string", cleaned.Password)
	}

	// sensitive:"redact" should be "[REDACTED]"
	if cleaned.APIKey != "[REDACTED]" {
		t.Errorf("APIKey = %q, want %q", cleaned.APIKey, "[REDACTED]")
	}

	// sensitive:"mask:***" should be "***"
	if cleaned.Token != "***" {
		t.Errorf("Token = %q, want %q", cleaned.Token, "***")
	}
}

func TestCleanSensitiveFields_PointerFields(t *testing.T) {
	type Config struct {
		Name     string  `json:"name"`
		Secret   *string `json:"secret" sensitive:"true"`
		APIToken *string `json:"api_token" sensitive:"redact"`
	}

	secret := "my-secret"
	token := "my-token"
	input := &Config{
		Name:     "test",
		Secret:   &secret,
		APIToken: &token,
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.(*Config)
	if !ok {
		t.Fatalf("Expected *Config, got %T", result)
	}

	if cleaned.Name != "test" {
		t.Errorf("Name = %q, want %q", cleaned.Name, "test")
	}

	// Pointer with sensitive:"true" should be nil
	if cleaned.Secret != nil {
		t.Errorf("Secret = %v, want nil", cleaned.Secret)
	}

	// Pointer string with sensitive:"redact" should be pointer to "[REDACTED]"
	if cleaned.APIToken == nil {
		t.Errorf("APIToken = nil, want pointer to [REDACTED]")
	} else if *cleaned.APIToken != "[REDACTED]" {
		t.Errorf("*APIToken = %q, want %q", *cleaned.APIToken, "[REDACTED]")
	}
}

func TestCleanSensitiveFields_NestedStruct(t *testing.T) {
	type Credentials struct {
		Username string `json:"username"`
		Password string `json:"password" sensitive:"true"`
	}

	type User struct {
		ID    string      `json:"id"`
		Creds Credentials `json:"credentials"`
	}

	input := &User{
		ID: "123",
		Creds: Credentials{
			Username: "john",
			Password: "secret",
		},
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.(*User)
	if !ok {
		t.Fatalf("Expected *User, got %T", result)
	}

	if cleaned.ID != "123" {
		t.Errorf("ID = %q, want %q", cleaned.ID, "123")
	}
	if cleaned.Creds.Username != "john" {
		t.Errorf("Creds.Username = %q, want %q", cleaned.Creds.Username, "john")
	}
	if cleaned.Creds.Password != "" {
		t.Errorf("Creds.Password = %q, want empty string", cleaned.Creds.Password)
	}
}

func TestCleanSensitiveFields_Slice(t *testing.T) {
	type Token struct {
		ID    string `json:"id"`
		Value string `json:"value" sensitive:"redact"`
	}

	input := []Token{
		{ID: "1", Value: "secret1"},
		{ID: "2", Value: "secret2"},
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.([]Token)
	if !ok {
		t.Fatalf("Expected []Token, got %T", result)
	}

	if len(cleaned) != 2 {
		t.Fatalf("len(cleaned) = %d, want 2", len(cleaned))
	}

	for i, token := range cleaned {
		expectedID := string(rune('1' + i))
		if token.ID != expectedID {
			t.Errorf("cleaned[%d].ID = %q, want %q", i, token.ID, expectedID)
		}
		if token.Value != "[REDACTED]" {
			t.Errorf("cleaned[%d].Value = %q, want %q", i, token.Value, "[REDACTED]")
		}
	}
}

func TestCleanSensitiveFields_SliceOfPointers(t *testing.T) {
	type Token struct {
		ID    string `json:"id"`
		Value string `json:"value" sensitive:"true"`
	}

	input := []*Token{
		{ID: "1", Value: "secret1"},
		{ID: "2", Value: "secret2"},
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.([]*Token)
	if !ok {
		t.Fatalf("Expected []*Token, got %T", result)
	}

	if len(cleaned) != 2 {
		t.Fatalf("len(cleaned) = %d, want 2", len(cleaned))
	}

	for i, token := range cleaned {
		if token == nil {
			t.Errorf("cleaned[%d] = nil, want non-nil", i)
			continue
		}
		if token.Value != "" {
			t.Errorf("cleaned[%d].Value = %q, want empty string", i, token.Value)
		}
	}
}

func TestCleanSensitiveFields_Map(t *testing.T) {
	type Secret struct {
		Name  string `json:"name"`
		Value string `json:"value" sensitive:"mask:****"`
	}

	input := map[string]Secret{
		"key1": {Name: "secret1", Value: "value1"},
		"key2": {Name: "secret2", Value: "value2"},
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.(map[string]Secret)
	if !ok {
		t.Fatalf("Expected map[string]Secret, got %T", result)
	}

	for key, secret := range cleaned {
		if secret.Value != "****" {
			t.Errorf("cleaned[%q].Value = %q, want %q", key, secret.Value, "****")
		}
	}
}

func TestCleanSensitiveFields_NilInput(t *testing.T) {
	result := CleanSensitiveFields(nil)
	if result != nil {
		t.Errorf("CleanSensitiveFields(nil) = %v, want nil", result)
	}
}

func TestCleanSensitiveFields_NonStruct(t *testing.T) {
	// Non-struct types should be returned as-is
	input := "hello"
	result := CleanSensitiveFields(input)
	if result != input {
		t.Errorf("CleanSensitiveFields(%q) = %v, want %q", input, result, input)
	}

	inputInt := 42
	resultInt := CleanSensitiveFields(inputInt)
	if resultInt != inputInt {
		t.Errorf("CleanSensitiveFields(%d) = %v, want %d", inputInt, resultInt, inputInt)
	}
}

func TestCleanSensitiveFields_NonStringTypes(t *testing.T) {
	type Config struct {
		PublicCount  int     `json:"public_count"`
		SecretCount  int     `json:"secret_count" sensitive:"true"`
		PrivateFloat float64 `json:"private_float" sensitive:"redact"`
	}

	input := &Config{
		PublicCount:  100,
		SecretCount:  42,
		PrivateFloat: 3.14,
	}

	result := CleanSensitiveFields(input)
	cleaned, ok := result.(*Config)
	if !ok {
		t.Fatalf("Expected *Config, got %T", result)
	}

	// Public field unchanged
	if cleaned.PublicCount != 100 {
		t.Errorf("PublicCount = %d, want 100", cleaned.PublicCount)
	}

	// Non-string types with sensitive tags get zero values
	if cleaned.SecretCount != 0 {
		t.Errorf("SecretCount = %d, want 0", cleaned.SecretCount)
	}

	// Redact on non-string types also gets zero value (can't put [REDACTED] in int)
	if cleaned.PrivateFloat != 0 {
		t.Errorf("PrivateFloat = %f, want 0", cleaned.PrivateFloat)
	}
}

func TestCleanSensitiveFields_UnexportedFields(t *testing.T) {
	type Config struct {
		Public  string `json:"public"`
		private string `json:"private" sensitive:"true"` //nolint:unused
	}

	input := &Config{
		Public: "visible",
	}

	// This should not panic and should work with exported fields only
	result := CleanSensitiveFields(input)
	cleaned, ok := result.(*Config)
	if !ok {
		t.Fatalf("Expected *Config, got %T", result)
	}

	if cleaned.Public != "visible" {
		t.Errorf("Public = %q, want %q", cleaned.Public, "visible")
	}
}

func TestProcessResponseValueWithSensitive_Integration(t *testing.T) {
	type Response struct {
		ID       string `json:"id"`
		Secret   string `json:"secret" sensitive:"redact"`
		CacheCtl string `header:"Cache-Control"`
	}

	headers := make(map[string]string)
	headerSetter := func(name, value string) {
		headers[name] = value
	}

	input := &Response{
		ID:       "123",
		Secret:   "super-secret",
		CacheCtl: "no-cache",
	}

	// With sensitive cleaning enabled
	result := ProcessResponseValueWithSensitive(input, headerSetter, true)
	cleaned, ok := result.(*Response)
	if !ok {
		t.Fatalf("Expected *Response, got %T", result)
	}

	if cleaned.Secret != "[REDACTED]" {
		t.Errorf("Secret = %q, want %q", cleaned.Secret, "[REDACTED]")
	}

	// Headers should still be set
	if headers["Cache-Control"] != "no-cache" {
		t.Errorf("Cache-Control header = %q, want %q", headers["Cache-Control"], "no-cache")
	}
}

func TestProcessResponseValueWithSensitive_Disabled(t *testing.T) {
	type Response struct {
		ID     string `json:"id"`
		Secret string `json:"secret" sensitive:"redact"`
	}

	input := &Response{
		ID:     "123",
		Secret: "super-secret",
	}

	// With sensitive cleaning disabled
	result := ProcessResponseValueWithSensitive(input, nil, false)
	returned, ok := result.(*Response)
	if !ok {
		t.Fatalf("Expected *Response, got %T", result)
	}

	// Secret should NOT be cleaned when disabled
	if returned.Secret != "super-secret" {
		t.Errorf("Secret = %q, want %q (should be unchanged when disabled)", returned.Secret, "super-secret")
	}
}
