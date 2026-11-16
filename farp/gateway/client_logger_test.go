package gateway

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/xraph/farp"

	"github.com/xraph/forge/farp/registry/memory"
)

// A schema skipped for a hash mismatch is logged when a logger is set.
func TestConvertToRoutesLogsSkippedSchemas(t *testing.T) {
	var buf bytes.Buffer

	logger := slog.New(slog.NewTextHandler(&buf, nil))
	bad := inlineManifest(t, "billing", "bill-1", openAPI("/invoices"), strings.Repeat("0", 64))

	c := NewClient(memory.NewRegistry(), WithLogger(logger))
	if routes := c.ConvertToRoutes([]*farp.SchemaManifest{bad}); len(routes) != 0 {
		t.Fatalf("mismatched schema produced routes %+v", routes)
	}

	out := buf.String()
	for _, want := range []string{"skipping schema", "instance_id=bill-1", "service=billing", "does not match"} {
		if !strings.Contains(out, want) {
			t.Fatalf("log %q is missing %q", out, want)
		}
	}

	// Without a logger the schema is still skipped, silently.
	if routes := NewClient(memory.NewRegistry()).ConvertToRoutes([]*farp.SchemaManifest{bad}); len(routes) != 0 {
		t.Fatalf("mismatched schema produced routes %+v", routes)
	}
}
