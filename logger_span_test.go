package forge

import (
	"context"
	"testing"
)

func TestSpanIDReexports(t *testing.T) {
	ctx := WithSpanID(context.Background(), "00f067aa0ba902b7")

	if SpanIDFromContext(ctx) != "00f067aa0ba902b7" {
		t.Fatal("span id did not round trip")
	}
}
