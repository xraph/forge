package vec

import (
	"sync/atomic"
	"testing"
)

type fake struct{ labels map[string]string }

func TestWithCachesChildren(t *testing.T) {
	var made atomic.Int32

	v := New("orders", []string{"tier", "region"}, func(l map[string]string) *fake {
		made.Add(1)

		return &fake{labels: l}
	}, &fake{})

	a := v.With("pro", "eu")
	b := v.With("pro", "eu")

	if a != b || made.Load() != 1 {
		t.Fatalf("children not cached: made %d", made.Load())
	}

	if a.labels["tier"] != "pro" || a.labels["region"] != "eu" {
		t.Fatalf("labels = %v", a.labels)
	}
}

func TestWrongArityReturnsSinkAndReportsOnce(t *testing.T) {
	sink := &fake{}
	calls := 0

	restore := SetArityHook(func(name string, want, got int) {
		if name != "orders" || want != 2 || got != 1 {
			t.Errorf("hook got %q %d %d", name, want, got)
		}

		calls++
	})
	defer restore()

	v := New("orders", []string{"tier", "region"}, func(map[string]string) *fake { return &fake{} }, sink)

	for range 2 {
		if v.With("pro") != sink {
			t.Fatal("wrong arity must return the sink")
		}
	}

	if calls != 1 {
		t.Fatalf("hook called %d times, want 1 per vector", calls)
	}
}

func TestCacheIsBounded(t *testing.T) {
	v := New("ids", []string{"id"}, func(map[string]string) *fake { return &fake{} }, &fake{})

	for i := range maxCached + 500 {
		v.With(string(rune('a'+i%26)) + string(rune(i)))
	}

	if n := v.cached.Load(); n > maxCached {
		t.Fatalf("cache grew to %d, cap is %d", n, maxCached)
	}
}
