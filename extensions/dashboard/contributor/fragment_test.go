package contributor

import (
	"testing"
)

func TestExtractBodyFragment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "full HTML document",
			input:    `<!DOCTYPE html><html><head><title>Test</title></head><body><div>Hello</div></body></html>`,
			expected: `<div>Hello</div>`,
		},
		{
			name:     "already a fragment",
			input:    `<div>Hello</div>`,
			expected: `<div>Hello</div>`,
		},
		{
			name:     "fragment with whitespace",
			input:    `  <div>Hello</div>  `,
			expected: `<div>Hello</div>`,
		},
		{
			name: "multiline document",
			input: `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Auth Dashboard</title>
  <link rel="stylesheet" href="/assets/style.css">
</head>
<body>
  <div class="container">
    <h1>Overview</h1>
    <p>Welcome</p>
  </div>
</body>
</html>`,
			expected: `<div class="container">
    <h1>Overview</h1>
    <p>Welcome</p>
  </div>`,
		},
		{
			name:     "body with attributes",
			input:    `<html><head></head><body class="dark" data-theme="forge"><p>Content</p></body></html>`,
			expected: `<p>Content</p>`,
		},
		{
			name:     "case insensitive tags",
			input:    `<!DOCTYPE HTML><HTML><HEAD></HEAD><BODY><span>test</span></BODY></HTML>`,
			expected: `<span>test</span>`,
		},
		{
			name:     "empty body",
			input:    `<!DOCTYPE html><html><head></head><body></body></html>`,
			expected: ``,
		},
		{
			name:     "no closing body tag",
			input:    `<!DOCTYPE html><html><head></head><body><p>unclosed</p></html>`,
			expected: `<p>unclosed</p></html>`,
		},
		{
			name:     "empty input",
			input:    ``,
			expected: ``,
		},
		{
			name: "astro static output example",
			input: `<!DOCTYPE html><html lang="en"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width"><link rel="stylesheet" href="/_astro/index.abc123.css"></head><body>
<div class="forge-page-title hidden" data-title="Auth Overview"></div>
<div class="space-y-6" x-data="authOverview()">
  <div class="grid grid-cols-3 gap-4">
    <div class="rounded-lg border p-4">
      <p class="text-sm">Active Sessions</p>
    </div>
  </div>
</div>
</body></html>`,
			expected: `<div class="forge-page-title hidden" data-title="Auth Overview"></div>
<div class="space-y-6" x-data="authOverview()">
  <div class="grid grid-cols-3 gap-4">
    <div class="rounded-lg border p-4">
      <p class="text-sm">Active Sessions</p>
    </div>
  </div>
</div>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractBodyFragment([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("ExtractBodyFragment():\ngot:  %q\nwant: %q", string(result), tt.expected)
			}
		})
	}
}

func TestIsFragment(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"fragment div", `<div>Hello</div>`, true},
		{"full document", `<html><body></body></html>`, false},
		{"only body", `<body><p>test</p></body>`, false},
		{"only html", `<html><p>test</p></html>`, false},
		{"empty", ``, true},
		{"plain text", `Hello world`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsFragment([]byte(tt.input))
			if result != tt.expected {
				t.Errorf("IsFragment(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestStripDocumentWrapper(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "full document",
			input:    `<!DOCTYPE html><html><head><title>Test</title></head><body><div>Hello</div></body></html>`,
			expected: `<div>Hello</div>`,
		},
		{
			name:     "fragment passthrough",
			input:    `<div>Hello</div>`,
			expected: `<div>Hello</div>`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := StripDocumentWrapper([]byte(tt.input))
			if string(result) != tt.expected {
				t.Errorf("StripDocumentWrapper():\ngot:  %q\nwant: %q", string(result), tt.expected)
			}
		})
	}
}
