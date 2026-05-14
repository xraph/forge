package idempotency

import (
	"encoding/json"
	"testing"
	"time"
)

func TestCached_FieldsRoundTrip(t *testing.T) {
	c := Cached{
		Status:   200,
		WireBody: json.RawMessage(`{"ok":true}`),
		StoredAt: time.Now(),
		TTL:      time.Hour,
	}
	if c.Status != 200 || string(c.WireBody) != `{"ok":true}` {
		t.Errorf("Cached fields not preserved: %+v", c)
	}
}

func TestCached_Expired(t *testing.T) {
	c := Cached{StoredAt: time.Now().Add(-2 * time.Hour), TTL: time.Hour}
	if !c.Expired(time.Now()) {
		t.Error("expected Expired() to be true")
	}
	c2 := Cached{StoredAt: time.Now(), TTL: time.Hour}
	if c2.Expired(time.Now()) {
		t.Error("expected fresh entry to not be expired")
	}
}
