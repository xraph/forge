package router

import (
	"errors"
	"testing"
)

// mockContext implements minimal Context for testing.
type mockInterceptorContext struct {
	values map[string]any
}

func newMockInterceptorContext() *mockInterceptorContext {
	return &mockInterceptorContext{
		values: make(map[string]any),
	}
}

func (m *mockInterceptorContext) Set(key string, value any) {
	m.values[key] = value
}

func (m *mockInterceptorContext) Get(key string) any {
	return m.values[key]
}

// TestAllow tests the Allow result.
func TestAllow(t *testing.T) {
	result := Allow()

	if result.Blocked {
		t.Error("Allow() should not be blocked")
	}

	if result.Error != nil {
		t.Error("Allow() should not have an error")
	}

	if len(result.Values) != 0 {
		t.Error("Allow() should not have values")
	}
}

// TestAllowWithValues tests the AllowWithValues result.
func TestAllowWithValues(t *testing.T) {
	values := map[string]any{"user": "test", "role": "admin"}
	result := AllowWithValues(values)

	if result.Blocked {
		t.Error("AllowWithValues() should not be blocked")
	}

	if result.Error != nil {
		t.Error("AllowWithValues() should not have an error")
	}

	if len(result.Values) != 2 {
		t.Errorf("AllowWithValues() should have 2 values, got %d", len(result.Values))
	}

	if result.Values["user"] != "test" {
		t.Error("AllowWithValues() should have user=test")
	}
}

// TestBlock tests the Block result.
func TestBlock(t *testing.T) {
	err := errors.New("access denied")
	result := Block(err)

	if !result.Blocked {
		t.Error("Block() should be blocked")
	}

	if !errors.Is(result.Error, err) {
		t.Error("Block() should have the error")
	}
}

// TestBlockWithValues tests the BlockWithValues result.
func TestBlockWithValues(t *testing.T) {
	err := errors.New("rate limited")
	values := map[string]any{"remaining": 0, "reset": 60}
	result := BlockWithValues(err, values)

	if !result.Blocked {
		t.Error("BlockWithValues() should be blocked")
	}

	if !errors.Is(result.Error, err) {
		t.Error("BlockWithValues() should have the error")
	}

	if len(result.Values) != 2 {
		t.Errorf("BlockWithValues() should have 2 values, got %d", len(result.Values))
	}
}

// TestNewInterceptor tests creating a named interceptor.
func TestNewInterceptor(t *testing.T) {
	interceptor := NewInterceptor("test-interceptor", func(ctx Context, route RouteInfo) InterceptorResult {
		return Allow()
	})

	if interceptor.Name() != "test-interceptor" {
		t.Errorf("Expected name 'test-interceptor', got '%s'", interceptor.Name())
	}
}

// TestInterceptorFromFunc tests creating an anonymous interceptor.
func TestInterceptorFromFunc(t *testing.T) {
	interceptor := InterceptorFromFunc(func(ctx Context, route RouteInfo) InterceptorResult {
		return Allow()
	})

	if interceptor.Name() != "" {
		t.Errorf("Expected empty name, got '%s'", interceptor.Name())
	}
}

// TestExecuteInterceptors tests the interceptor executor.
func TestExecuteInterceptors(t *testing.T) {
	t.Run("empty interceptors", func(t *testing.T) {
		ctx := newMockInterceptorContext()

		err := executeInterceptors(nil, RouteInfo{}, nil, nil)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}

		_ = ctx // suppress unused warning
	})

	t.Run("all allow", func(t *testing.T) {
		interceptors := []Interceptor{
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult {
				return Allow()
			}),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult {
				return Allow()
			}),
		}

		err := executeInterceptors(nil, RouteInfo{}, interceptors, nil)
		if err != nil {
			t.Errorf("Expected no error, got %v", err)
		}
	})

	t.Run("one blocks", func(t *testing.T) {
		blockErr := errors.New("blocked")
		interceptors := []Interceptor{
			NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult {
				return Allow()
			}),
			NewInterceptor("i2", func(ctx Context, route RouteInfo) InterceptorResult {
				return Block(blockErr)
			}),
			NewInterceptor("i3", func(ctx Context, route RouteInfo) InterceptorResult {
				return Allow()
			}),
		}

		err := executeInterceptors(nil, RouteInfo{}, interceptors, nil)

		if !errors.Is(err, blockErr) {
			t.Errorf("Expected block error, got %v", err)
		}
	})

	t.Run("skip interceptor by name", func(t *testing.T) {
		blockErr := errors.New("should be skipped")
		interceptors := []Interceptor{
			NewInterceptor("skip-me", func(ctx Context, route RouteInfo) InterceptorResult {
				return Block(blockErr)
			}),
			NewInterceptor("run-me", func(ctx Context, route RouteInfo) InterceptorResult {
				return Allow()
			}),
		}

		skipSet := map[string]bool{"skip-me": true}

		err := executeInterceptors(nil, RouteInfo{}, interceptors, skipSet)
		if err != nil {
			t.Errorf("Expected no error (skip-me should be skipped), got %v", err)
		}
	})
}

// TestCombineSkipSets tests combining multiple skip sets.
func TestCombineSkipSets(t *testing.T) {
	set1 := map[string]bool{"a": true, "b": true}
	set2 := map[string]bool{"c": true}
	set3 := map[string]bool{"a": true, "d": true} // duplicate 'a'

	combined := combineSkipSets(set1, set2, set3)

	expected := []string{"a", "b", "c", "d"}
	for _, key := range expected {
		if !combined[key] {
			t.Errorf("Expected key '%s' in combined set", key)
		}
	}

	if len(combined) != 4 {
		t.Errorf("Expected 4 keys, got %d", len(combined))
	}
}

// TestCombineInterceptors tests combining group and route interceptors.
func TestCombineInterceptors(t *testing.T) {
	group := []Interceptor{
		NewInterceptor("g1", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
		NewInterceptor("g2", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
	}

	route := []Interceptor{
		NewInterceptor("r1", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
	}

	t.Run("both non-empty", func(t *testing.T) {
		combined := combineInterceptors(group, route)

		if len(combined) != 3 {
			t.Errorf("Expected 3 interceptors, got %d", len(combined))
		}

		// Group interceptors should come first
		if combined[0].Name() != "g1" {
			t.Error("First interceptor should be g1")
		}

		if combined[2].Name() != "r1" {
			t.Error("Last interceptor should be r1")
		}
	})

	t.Run("empty group", func(t *testing.T) {
		combined := combineInterceptors(nil, route)

		if len(combined) != 1 {
			t.Errorf("Expected 1 interceptor, got %d", len(combined))
		}
	})

	t.Run("empty route", func(t *testing.T) {
		combined := combineInterceptors(group, nil)

		if len(combined) != 2 {
			t.Errorf("Expected 2 interceptors, got %d", len(combined))
		}
	})
}

// TestInterceptorChain tests the InterceptorChain wrapper.
func TestInterceptorChain(t *testing.T) {
	interceptors := []Interceptor{
		NewInterceptor("i1", func(ctx Context, route RouteInfo) InterceptorResult { return Allow() }),
	}

	chain := NewInterceptorChain(interceptors, nil)

	if chain.Empty() {
		t.Error("Chain should not be empty")
	}

	if chain.Len() != 1 {
		t.Errorf("Expected length 1, got %d", chain.Len())
	}

	emptyChain := NewInterceptorChain(nil, nil)
	if !emptyChain.Empty() {
		t.Error("Empty chain should be empty")
	}
}
