package dataloader

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNewLoader(t *testing.T) {
	config := DefaultLoaderConfig()
	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		return keys, make([]error, len(keys))
	}

	loader := NewLoader(config, batchFunc)
	if loader == nil {
		t.Fatal("NewLoader returned nil")
	}
}

func TestLoader_Load(t *testing.T) {
	config := DefaultLoaderConfig()
	config.Wait = 10 * time.Millisecond

	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		values := make([]interface{}, len(keys))
		errs := make([]error, len(keys))
		for i, key := range keys {
			values[i] = key.(int) * 2
		}
		return values, errs
	}

	loader := NewLoader(config, batchFunc)
	ctx := context.Background()

	// Load a value
	val, err := loader.Load(ctx, 5)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if val.(int) != 10 {
		t.Errorf("expected 10, got %v", val)
	}
}

func TestLoader_LoadMany(t *testing.T) {
	config := DefaultLoaderConfig()
	config.Wait = 10 * time.Millisecond

	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		values := make([]interface{}, len(keys))
		errs := make([]error, len(keys))
		for i, key := range keys {
			values[i] = key.(int) * 2
		}
		return values, errs
	}

	loader := NewLoader(config, batchFunc)
	ctx := context.Background()

	// Load multiple values
	keys := []interface{}{1, 2, 3}
	values, err := loader.LoadMany(ctx, keys)
	if err != nil {
		t.Fatalf("LoadMany failed: %v", err)
	}

	expected := []interface{}{2, 4, 6}
	for i, val := range values {
		if val != expected[i] {
			t.Errorf("expected %v, got %v at index %d", expected[i], val, i)
		}
	}
}

func TestLoader_Prime(t *testing.T) {
	config := DefaultLoaderConfig()
	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		return keys, make([]error, len(keys))
	}

	loader := NewLoader(config, batchFunc)
	ctx := context.Background()

	// Prime the cache
	loader.Prime(5, 100)

	// Load should return cached value immediately
	val, err := loader.Load(ctx, 5)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if val.(int) != 100 {
		t.Errorf("expected 100 from cache, got %v", val)
	}
}

func TestLoader_Clear(t *testing.T) {
	config := DefaultLoaderConfig()
	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		return keys, make([]error, len(keys))
	}

	loader := NewLoader(config, batchFunc)

	// Prime and clear
	loader.Prime(5, 100)
	loader.Clear(5)

	// Cache should be cleared
	loader.cacheMu.RLock()
	_, ok := loader.cache[5]
	loader.cacheMu.RUnlock()

	if ok {
		t.Error("expected cache to be cleared")
	}
}

func TestLoader_ClearAll(t *testing.T) {
	config := DefaultLoaderConfig()
	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		return keys, make([]error, len(keys))
	}

	loader := NewLoader(config, batchFunc)

	// Prime multiple values
	loader.Prime(1, 10)
	loader.Prime(2, 20)
	loader.Prime(3, 30)

	// Clear all
	loader.ClearAll()

	// Cache should be empty
	loader.cacheMu.RLock()
	cacheLen := len(loader.cache)
	loader.cacheMu.RUnlock()

	if cacheLen != 0 {
		t.Errorf("expected empty cache, got %d entries", cacheLen)
	}
}

func TestLoader_WithErrors(t *testing.T) {
	config := DefaultLoaderConfig()
	config.Wait = 10 * time.Millisecond

	testErr := errors.New("batch error")
	batchFunc := func(ctx context.Context, keys []interface{}) ([]interface{}, []error) {
		values := make([]interface{}, len(keys))
		errs := make([]error, len(keys))
		for i := range keys {
			errs[i] = testErr
		}
		return values, errs
	}

	loader := NewLoader(config, batchFunc)
	ctx := context.Background()

	// Load should return error
	_, err := loader.Load(ctx, 5)
	if err != testErr {
		t.Errorf("expected test error, got %v", err)
	}
}
