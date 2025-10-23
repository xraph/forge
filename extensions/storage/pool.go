package storage

import (
	"bytes"
	"sync"
)

// BufferPool manages a pool of reusable buffers for efficient I/O operations
type BufferPool struct {
	pool *sync.Pool
}

// NewBufferPool creates a new buffer pool with the specified buffer size
func NewBufferPool(bufferSize int) *BufferPool {
	return &BufferPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return make([]byte, bufferSize)
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (bp *BufferPool) Get() []byte {
	return bp.pool.Get().([]byte)
}

// Put returns a buffer to the pool
func (bp *BufferPool) Put(buf []byte) {
	bp.pool.Put(buf)
}

// BytesBufferPool manages a pool of bytes.Buffer for efficient operations
type BytesBufferPool struct {
	pool *sync.Pool
}

// NewBytesBufferPool creates a new bytes buffer pool
func NewBytesBufferPool() *BytesBufferPool {
	return &BytesBufferPool{
		pool: &sync.Pool{
			New: func() interface{} {
				return new(bytes.Buffer)
			},
		},
	}
}

// Get retrieves a buffer from the pool
func (bbp *BytesBufferPool) Get() *bytes.Buffer {
	buf := bbp.pool.Get().(*bytes.Buffer)
	buf.Reset()
	return buf
}

// Put returns a buffer to the pool
func (bbp *BytesBufferPool) Put(buf *bytes.Buffer) {
	// Reset buffer before returning to pool
	buf.Reset()
	bbp.pool.Put(buf)
}
