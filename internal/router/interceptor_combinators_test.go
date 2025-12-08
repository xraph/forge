package router

import (
	"errors"
	"testing"
)

// TestAnd tests the And combinator.
func TestAnd(t *testing.T) {
	t.Run("all allow", func(t *testing.T) {
		interceptor := And(
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("And() with all allowing should not block")
		}
	})

	t.Run("one blocks", func(t *testing.T) {
		blockErr := errors.New("blocked")
		interceptor := And(
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult { return Block(blockErr) }),
			NewInterceptor("i3", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if !result.Blocked {
			t.Error("And() with one blocking should block")
		}

		if result.Error != blockErr {
			t.Error("And() should return the blocking error")
		}
	})

	t.Run("empty", func(t *testing.T) {
		interceptor := And()
		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("And() with no interceptors should allow")
		}
	})
}

// TestOr tests the Or combinator.
func TestOr(t *testing.T) {
	t.Run("first allows", func(t *testing.T) {
		interceptor := Or(
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("blocked")) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("Or() with first allowing should not block")
		}
	})

	t.Run("second allows", func(t *testing.T) {
		interceptor := Or(
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("blocked1")) }),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("Or() with second allowing should not block")
		}
	})

	t.Run("all block", func(t *testing.T) {
		lastErr := errors.New("last error")
		interceptor := Or(
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("first")) }),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult { return Block(lastErr) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if !result.Blocked {
			t.Error("Or() with all blocking should block")
		}

		if result.Error != lastErr {
			t.Error("Or() should return the last error")
		}
	})

	t.Run("empty", func(t *testing.T) {
		interceptor := Or()
		result := interceptor.Intercept(nil, RouteInfo{})

		if !result.Blocked {
			t.Error("Or() with no interceptors should block (no success)")
		}
	})
}

// TestNot tests the Not combinator.
func TestNot(t *testing.T) {
	t.Run("inverts allow to block", func(t *testing.T) {
		blockErr := errors.New("inverted")
		interceptor := Not(
			NewInterceptor("allow", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
			blockErr,
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if !result.Blocked {
			t.Error("Not() should invert allow to block")
		}

		if result.Error != blockErr {
			t.Error("Not() should use provided error")
		}
	})

	t.Run("inverts block to allow", func(t *testing.T) {
		interceptor := Not(
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("original")) }),
			errors.New("unused"),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("Not() should invert block to allow")
		}
	})

	t.Run("has proper name", func(t *testing.T) {
		interceptor := Not(
			NewInterceptor("inner", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
			errors.New("err"),
		)

		if interceptor.Name() != "not:inner" {
			t.Errorf("Expected name 'not:inner', got '%s'", interceptor.Name())
		}
	})
}

// TestWhen tests the When combinator.
func TestWhen(t *testing.T) {
	t.Run("predicate true - runs interceptor", func(t *testing.T) {
		blockErr := errors.New("blocked")
		interceptor := When(
			func(ctx Context, route RouteInfo) bool { return true },
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(blockErr) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if !result.Blocked {
			t.Error("When() with true predicate should run interceptor")
		}
	})

	t.Run("predicate false - skips interceptor", func(t *testing.T) {
		interceptor := When(
			func(ctx Context, route RouteInfo) bool { return false },
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("should not happen")) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("When() with false predicate should skip interceptor")
		}
	})
}

// TestUnless tests the Unless combinator.
func TestUnless(t *testing.T) {
	t.Run("predicate true - skips interceptor", func(t *testing.T) {
		interceptor := Unless(
			func(ctx Context, route RouteInfo) bool { return true },
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("should not happen")) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("Unless() with true predicate should skip interceptor")
		}
	})

	t.Run("predicate false - runs interceptor", func(t *testing.T) {
		blockErr := errors.New("blocked")
		interceptor := Unless(
			func(ctx Context, route RouteInfo) bool { return false },
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(blockErr) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if !result.Blocked {
			t.Error("Unless() with false predicate should run interceptor")
		}
	})
}

// TestIfMetadata tests the IfMetadata combinator.
func TestIfMetadata(t *testing.T) {
	t.Run("metadata matches - runs interceptor", func(t *testing.T) {
		blockErr := errors.New("blocked")
		interceptor := IfMetadata("key", "value",
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(blockErr) }),
		)

		route := RouteInfo{
			Metadata: map[string]any{"key": "value"},
		}

		result := interceptor.Intercept(nil, route)

		if !result.Blocked {
			t.Error("IfMetadata() with matching metadata should run interceptor")
		}
	})

	t.Run("metadata does not match - skips interceptor", func(t *testing.T) {
		interceptor := IfMetadata("key", "value",
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("should not happen")) }),
		)

		route := RouteInfo{
			Metadata: map[string]any{"key": "other"},
		}

		result := interceptor.Intercept(nil, route)

		if result.Blocked {
			t.Error("IfMetadata() with non-matching metadata should skip interceptor")
		}
	})

	t.Run("no metadata - skips interceptor", func(t *testing.T) {
		interceptor := IfMetadata("key", "value",
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("should not happen")) }),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("IfMetadata() with no metadata should skip interceptor")
		}
	})
}

// TestIfTag tests the IfTag combinator.
func TestIfTag(t *testing.T) {
	t.Run("tag present - runs interceptor", func(t *testing.T) {
		blockErr := errors.New("blocked")
		interceptor := IfTag("admin",
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(blockErr) }),
		)

		route := RouteInfo{
			Tags: []string{"public", "admin", "api"},
		}

		result := interceptor.Intercept(nil, route)

		if !result.Blocked {
			t.Error("IfTag() with matching tag should run interceptor")
		}
	})

	t.Run("tag not present - skips interceptor", func(t *testing.T) {
		interceptor := IfTag("admin",
			NewInterceptor("block", func(ctx Context, route RouteInfo) InterceptorResult { return Block(errors.New("should not happen")) }),
		)

		route := RouteInfo{
			Tags: []string{"public", "api"},
		}

		result := interceptor.Intercept(nil, route)

		if result.Blocked {
			t.Error("IfTag() with non-matching tag should skip interceptor")
		}
	})
}

// TestChainInterceptors tests the ChainInterceptors function.
func TestChainInterceptors(t *testing.T) {
	t.Run("chains multiple", func(t *testing.T) {
		callOrder := []string{}

		interceptor := ChainInterceptors(
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult {
				callOrder = append(callOrder, "i1")
				return Allow()
			}),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult {
				callOrder = append(callOrder, "i2")
				return Allow()
			}),
			NewInterceptor("i3", func(ctx Context, route RouteInfo) InterceptorResult {
				callOrder = append(callOrder, "i3")
				return Allow()
			}),
		)

		result := interceptor.Intercept(nil, RouteInfo{})

		if result.Blocked {
			t.Error("ChainInterceptors() with all allowing should not block")
		}

		if len(callOrder) != 3 {
			t.Errorf("Expected 3 calls, got %d", len(callOrder))
		}

		expected := []string{"i1", "i2", "i3"}
		for i, exp := range expected {
			if callOrder[i] != exp {
				t.Errorf("Expected call %d to be '%s', got '%s'", i, exp, callOrder[i])
			}
		}
	})
}

