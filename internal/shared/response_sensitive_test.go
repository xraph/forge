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
