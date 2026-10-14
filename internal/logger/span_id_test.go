package logger

import (
	"context"
	"testing"
)

func TestSpanIDReexports(t *testing.T) {
	ctx := WithSpanID(context.Background(), "00f067aa0ba902b7")

	if got := SpanIDFromContext(ctx); got != "00f067aa0ba902b7" {
		t.Fatalf("SpanIDFromContext = %q", got)
	}

	found := false

	for _, f := range ContextFields(ctx) {
		if f.Key() == "span_id" {
			found = true
		}
	}

	if !found {
		t.Fatal("ContextFields did not include span_id")
	}
}
