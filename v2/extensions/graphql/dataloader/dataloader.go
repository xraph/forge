package dataloader

import (
	"context"
	"sync"
	"time"
)

// LoaderConfig configures a DataLoader
type LoaderConfig struct {
	// Wait is the duration to wait before dispatching a batch
	Wait time.Duration
	// MaxBatch is the maximum number of keys to load in a single batch
	MaxBatch int
}

// DefaultLoaderConfig returns default loader configuration
func DefaultLoaderConfig() LoaderConfig {
	return LoaderConfig{
		Wait:     10 * time.Millisecond,
		MaxBatch: 100,
	}
}

// BatchFunc loads multiple keys and returns their values
// The returned slice must be the same length as the keys slice
type BatchFunc func(ctx context.Context, keys []interface{}) ([]interface{}, []error)

// Loader batches and caches data loading operations
type Loader struct {
	config    LoaderConfig
	batchFunc BatchFunc
	cache     map[interface{}]interface{}
	cacheMu   sync.RWMutex
	batch     []interface{}
	batchMu   sync.Mutex
	scheduled bool
	results   map[interface{}]chan result
}

type result struct {
	value interface{}
	err   error
}

// NewLoader creates a new DataLoader
func NewLoader(config LoaderConfig, batchFunc BatchFunc) *Loader {
	return &Loader{
		config:    config,
		batchFunc: batchFunc,
		cache:     make(map[interface{}]interface{}),
		results:   make(map[interface{}]chan result),
	}
}

// Load loads a single key, batching and caching the load
func (l *Loader) Load(ctx context.Context, key interface{}) (interface{}, error) {
	// Check cache first
	l.cacheMu.RLock()
	if val, ok := l.cache[key]; ok {
		l.cacheMu.RUnlock()
		return val, nil
	}
	l.cacheMu.RUnlock()

	// Add to batch
	l.batchMu.Lock()

	// Check if we already have a result channel for this key
	resultCh, exists := l.results[key]
	if !exists {
		resultCh = make(chan result, 1)
		l.results[key] = resultCh
		l.batch = append(l.batch, key)
	}

	// Schedule batch dispatch if not already scheduled
	if !l.scheduled {
		l.scheduled = true
		go l.dispatch(ctx)
	}

	l.batchMu.Unlock()

	// Wait for result
	res := <-resultCh
	return res.value, res.err
}

// LoadMany loads multiple keys at once
func (l *Loader) LoadMany(ctx context.Context, keys []interface{}) ([]interface{}, error) {
	results := make([]interface{}, len(keys))
	errs := make([]error, len(keys))

	for i, key := range keys {
		val, err := l.Load(ctx, key)
		results[i] = val
		errs[i] = err
	}

	// Return first error if any
	for _, err := range errs {
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// Prime adds a value to the cache
func (l *Loader) Prime(key interface{}, value interface{}) {
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	l.cache[key] = value
}

// Clear removes a key from the cache
func (l *Loader) Clear(key interface{}) {
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	delete(l.cache, key)
}

// ClearAll removes all keys from the cache
func (l *Loader) ClearAll() {
	l.cacheMu.Lock()
	defer l.cacheMu.Unlock()
	l.cache = make(map[interface{}]interface{})
}

// dispatch executes the batch function after waiting
func (l *Loader) dispatch(ctx context.Context) {
	// Wait for the configured duration or until max batch size
	time.Sleep(l.config.Wait)

	l.batchMu.Lock()
	keys := l.batch
	resultChans := l.results
	l.batch = nil
	l.results = make(map[interface{}]chan result)
	l.scheduled = false
	l.batchMu.Unlock()

	if len(keys) == 0 {
		return
	}

	// Execute batch function
	values, errs := l.batchFunc(ctx, keys)

	// Send results to waiting goroutines
	for i, key := range keys {
		var res result
		if i < len(values) {
			res.value = values[i]
		}
		if i < len(errs) {
			res.err = errs[i]
		}

		// Cache successful results
		if res.err == nil && res.value != nil {
			l.cacheMu.Lock()
			l.cache[key] = res.value
			l.cacheMu.Unlock()
		}

		// Send result
		if ch, ok := resultChans[key]; ok {
			ch <- res
			close(ch)
		}
	}
}
