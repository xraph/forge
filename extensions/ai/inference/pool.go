package inference

import (
	"fmt"
	"sync"
	"time"

	"github.com/xraph/forge/extensions/ai/models"
)

// ObjectPool provides efficient object pooling for AI inference pipeline.
type ObjectPool struct {
	// Request pools
	requestPool  sync.Pool
	responsePool sync.Pool
	batchPool    sync.Pool

	// Worker pools
	workerPool  sync.Pool
	contextPool sync.Pool

	// Buffer pools for high-frequency allocations
	bufferPool sync.Pool
	stringPool sync.Pool

	// Configuration
	maxPoolSize     int
	cleanupInterval time.Duration
	stats           PoolStats
	mu              sync.RWMutex
}

// PoolStats tracks object pool statistics.
type PoolStats struct {
	RequestHits      int64     `json:"request_hits"`
	RequestMisses    int64     `json:"request_misses"`
	ResponseHits     int64     `json:"response_hits"`
	ResponseMisses   int64     `json:"response_misses"`
	BatchHits        int64     `json:"batch_hits"`
	BatchMisses      int64     `json:"batch_misses"`
	WorkerHits       int64     `json:"worker_hits"`
	WorkerMisses     int64     `json:"worker_misses"`
	BufferHits       int64     `json:"buffer_hits"`
	BufferMisses     int64     `json:"buffer_misses"`
	TotalAllocations int64     `json:"total_allocations"`
	TotalReuses      int64     `json:"total_reuses"`
	LastCleanup      time.Time `json:"last_cleanup"`
}

// NewObjectPool creates a new object pool for AI inference.
func NewObjectPool(maxPoolSize int, cleanupInterval time.Duration) *ObjectPool {
	pool := &ObjectPool{
		maxPoolSize:     maxPoolSize,
		cleanupInterval: cleanupInterval,
		stats: PoolStats{
			LastCleanup: time.Now(),
		},
	}

	// Initialize request pool
	pool.requestPool = sync.Pool{
		New: func() any {
			return &InferenceRequest{
				Context: make(map[string]any),
				Options: InferenceOptions{},
			}
		},
	}

	// Initialize response pool
	pool.responsePool = sync.Pool{
		New: func() any {
			return &InferenceResponse{
				Metadata: make(map[string]any),
			}
		},
	}

	// Initialize worker pool
	pool.workerPool = sync.Pool{
		New: func() any {
			return &InferenceWorker{
				id: generateWorkerID(),
			}
		},
	}

	// Initialize context pool
	pool.contextPool = sync.Pool{
		New: func() any {
			return make(map[string]any, 10)
		},
	}

	// Initialize buffer pool
	pool.bufferPool = sync.Pool{
		New: func() any {
			return make([]byte, 0, 4096) // 4KB initial capacity
		},
	}

	// Initialize string pool
	pool.stringPool = sync.Pool{
		New: func() any {
			return make([]string, 0, 10)
		},
	}

	return pool
}

// GetRequest gets a request from the pool or creates a new one.
func (p *ObjectPool) GetRequest() *InferenceRequest {
	obj := p.requestPool.Get()
	if obj == nil {
		p.mu.Lock()
		p.stats.RequestMisses++
		p.stats.TotalAllocations++
		p.mu.Unlock()

		return &InferenceRequest{
			Context: make(map[string]any),
			Options: InferenceOptions{},
		}
	}

	p.mu.Lock()
	p.stats.RequestHits++
	p.stats.TotalReuses++
	p.mu.Unlock()

	request := obj.(*InferenceRequest)
	p.resetRequest(request)

	return request
}

// PutRequest returns a request to the pool.
func (p *ObjectPool) PutRequest(request *InferenceRequest) {
	if request == nil {
		return
	}

	p.requestPool.Put(request)
}

// GetResponse gets a response from the pool or creates a new one.
func (p *ObjectPool) GetResponse() *InferenceResponse {
	obj := p.responsePool.Get()
	if obj == nil {
		p.mu.Lock()
		p.stats.ResponseMisses++
		p.stats.TotalAllocations++
		p.mu.Unlock()

		return &InferenceResponse{
			Metadata: make(map[string]any),
		}
	}

	p.mu.Lock()
	p.stats.ResponseHits++
	p.stats.TotalReuses++
	p.mu.Unlock()

	response := obj.(*InferenceResponse)
	p.resetResponse(response)

	return response
}

// PutResponse returns a response to the pool.
func (p *ObjectPool) PutResponse(response *InferenceResponse) {
	if response == nil {
		return
	}

	p.responsePool.Put(response)
}

// GetWorker gets a worker from the pool or creates a new one.
func (p *ObjectPool) GetWorker() *InferenceWorker {
	obj := p.workerPool.Get()
	if obj == nil {
		p.mu.Lock()
		p.stats.WorkerMisses++
		p.stats.TotalAllocations++
		p.mu.Unlock()

		return &InferenceWorker{
			id: generateWorkerID(),
		}
	}

	p.mu.Lock()
	p.stats.WorkerHits++
	p.stats.TotalReuses++
	p.mu.Unlock()

	worker := obj.(*InferenceWorker)
	p.resetWorker(worker)

	return worker
}

// PutWorker returns a worker to the pool.
func (p *ObjectPool) PutWorker(worker *InferenceWorker) {
	if worker == nil {
		return
	}

	p.workerPool.Put(worker)
}

// GetBuffer gets a buffer from the pool or creates a new one.
func (p *ObjectPool) GetBuffer() []byte {
	obj := p.bufferPool.Get()
	if obj == nil {
		p.mu.Lock()
		p.stats.BufferMisses++
		p.stats.TotalAllocations++
		p.mu.Unlock()

		return make([]byte, 0, 4096)
	}

	p.mu.Lock()
	p.stats.BufferHits++
	p.stats.TotalReuses++
	p.mu.Unlock()

	buffer := obj.([]byte)

	return buffer[:0] // Reset length but keep capacity
}

// PutBuffer returns a buffer to the pool.
func (p *ObjectPool) PutBuffer(buffer []byte) {
	if buffer == nil {
		return
	}
	// Only pool buffers with reasonable capacity
	if cap(buffer) <= 65536 { // 64KB max
		p.bufferPool.Put(&buffer)
	}
}

// GetStringSlice gets a string slice from the pool or creates a new one.
func (p *ObjectPool) GetStringSlice() []string {
	obj := p.stringPool.Get()
	if obj == nil {
		return make([]string, 0, 10)
	}

	slice := obj.([]string)

	return slice[:0] // Reset length but keep capacity
}

// PutStringSlice returns a string slice to the pool.
func (p *ObjectPool) PutStringSlice(slice []string) {
	if slice == nil {
		return
	}

	p.stringPool.Put(slice)
}

// GetContext gets a context map from the pool or creates a new one.
func (p *ObjectPool) GetContext() map[string]any {
	obj := p.contextPool.Get()
	if obj == nil {
		return make(map[string]any, 10)
	}

	context := obj.(map[string]any)
	// Clear the map
	for k := range context {
		delete(context, k)
	}

	return context
}

// PutContext returns a context map to the pool.
func (p *ObjectPool) PutContext(context map[string]any) {
	if context == nil {
		return
	}

	p.contextPool.Put(context)
}

// GetStats returns pool statistics.
func (p *ObjectPool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return p.stats
}

// ResetStats resets pool statistics.
func (p *ObjectPool) ResetStats() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.stats = PoolStats{
		LastCleanup: time.Now(),
	}
}

// Helper methods for resetting objects

func (p *ObjectPool) resetRequest(request *InferenceRequest) {
	request.ID = ""
	request.ModelID = ""
	request.Input = models.ModelInput{}
	request.Options = InferenceOptions{}
	request.Timeout = 0
	request.Timestamp = time.Time{}
	request.Priority = 0
	request.Callback = nil

	// Clear context map
	for k := range request.Context {
		delete(request.Context, k)
	}
}

func (p *ObjectPool) resetResponse(response *InferenceResponse) {
	response.ID = ""
	response.ModelID = ""
	response.Output = models.ModelOutput{}
	response.Error = nil
	response.Timestamp = time.Time{}
	response.Metadata = nil

	// Clear metadata map
	for k := range response.Metadata {
		delete(response.Metadata, k)
	}
}

func (p *ObjectPool) resetWorker(worker *InferenceWorker) {
	worker.id = generateWorkerID()
	// Reset other fields as needed
}

// generateWorkerID generates a unique worker ID.
func generateWorkerID() string {
	return fmt.Sprintf("worker_%d", time.Now().UnixNano())
}
