package contract

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestLogAuditEmitter_FormatsRecord(t *testing.T) {
	var buf bytes.Buffer
	em := NewLogAuditEmitter(&buf)
	em.Emit(context.Background(), AuditRecord{
		Time:        time.Now(),
		Contributor: "users",
		Intent:      "user.disable",
		Subject:     "u_42",
		User:        "admin@example.com",
		Result:      "ok",
		LatencyMs:   12,
	})
	out := buf.String()
	for _, want := range []string{"users", "user.disable", "u_42", "admin@example.com", "ok"} {
		if !strings.Contains(out, want) {
			t.Errorf("audit output missing %q: %s", want, out)
		}
	}
}

func TestNoopAuditEmitter_DoesNothing(t *testing.T) {
	em := NoopAuditEmitter{}
	em.Emit(context.Background(), AuditRecord{}) // must not panic
}
