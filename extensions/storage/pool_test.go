package storage

import (
	"sync"
	"testing"
)

func TestBufferPool(t *testing.T) {
	bufferSize := 1024
	pool := NewBufferPool(bufferSize)

	// Test: Get buffer
	buf := pool.Get()
	if len(buf) != bufferSize {
		t.Errorf("Expected buffer size %d, got %d", bufferSize, len(buf))
	}

	// Test: Put buffer back
	pool.Put(buf)

	// Test: Reuse buffer
	buf2 := pool.Get()
	if len(buf2) != bufferSize {
		t.Errorf("Expected buffer size %d, got %d", bufferSize, len(buf2))
	}
}

func TestBufferPool_Concurrent(t *testing.T) {
	bufferSize := 1024
	pool := NewBufferPool(bufferSize)

	var wg sync.WaitGroup

	iterations := 100
	goroutines := 10

	for range goroutines {

		wg.Go(func() {

			for range iterations {
				buf := pool.Get()
				// Use buffer
				for k := range buf {
					buf[k] = byte(k)
				}

				pool.Put(buf)
			}
		})
	}

	wg.Wait()
}

func TestBytesBufferPool(t *testing.T) {
	pool := NewBytesBufferPool()

	// Test: Get buffer
	buf := pool.Get()
	if buf == nil {
		t.Error("Expected non-nil buffer")
	}

	if buf.Len() != 0 {
		t.Errorf("Expected empty buffer, got length %d", buf.Len())
	}

	// Test: Use buffer
	buf.WriteString("test data")

	if buf.Len() != 9 {
		t.Errorf("Expected length 9, got %d", buf.Len())
	}

	// Test: Put buffer back
	pool.Put(buf)

	// Test: Reuse buffer (should be reset)
	buf2 := pool.Get()
	if buf2.Len() != 0 {
		t.Errorf("Expected reset buffer, got length %d", buf2.Len())
	}
}

func TestBytesBufferPool_Concurrent(t *testing.T) {
	pool := NewBytesBufferPool()

	var wg sync.WaitGroup

	iterations := 100
	goroutines := 10

	for i := range goroutines {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			for j := range iterations {
				buf := pool.Get()
				buf.WriteString("test data")

				if buf.Len() == 0 {
					t.Errorf("Buffer should have data, goroutine %d iteration %d", id, j)
				}

				pool.Put(buf)
			}
		}(i)
	}

	wg.Wait()
}

func BenchmarkBufferPool(b *testing.B) {
	pool := NewBufferPool(4096)

	for b.Loop() {
		buf := pool.Get()
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_NoPool(b *testing.B) {

	for b.Loop() {
		buf := make([]byte, 4096)
		_ = buf
	}
}

func BenchmarkBytesBufferPool(b *testing.B) {
	pool := NewBytesBufferPool()

	for b.Loop() {
		buf := pool.Get()
		buf.WriteString("test data")
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_Parallel(b *testing.B) {
	pool := NewBufferPool(4096)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			pool.Put(buf)
		}
	})
}
