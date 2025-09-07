package memory

import (
	"container/heap"
	"sync"
)

// LFUNode represents a node in the LFU cache
type LFUNode struct {
	key       string
	value     interface{}
	frequency int64
	index     int // Index in the heap
}

// LFUHeap implements a min-heap for LFU eviction
type LFUHeap struct {
	nodes   []*LFUNode
	nodeMap map[string]*LFUNode
	mu      sync.RWMutex
	minFreq int64
}

// NewLFUHeap creates a new LFU heap
func NewLFUHeap() *LFUHeap {
	return &LFUHeap{
		nodes:   make([]*LFUNode, 0),
		nodeMap: make(map[string]*LFUNode),
		minFreq: 1,
	}
}

// Len returns the number of nodes in the heap
func (h *LFUHeap) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.nodes)
}

// Less compares two nodes based on frequency (min-heap)
func (h *LFUHeap) Less(i, j int) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if h.nodes[i].frequency == h.nodes[j].frequency {
		// If frequencies are equal, prioritize by insertion order (FIFO)
		return h.nodes[i].index < h.nodes[j].index
	}
	return h.nodes[i].frequency < h.nodes[j].frequency
}

// Swap swaps two nodes in the heap
func (h *LFUHeap) Swap(i, j int) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nodes[i], h.nodes[j] = h.nodes[j], h.nodes[i]
	h.nodes[i].index = i
	h.nodes[j].index = j
}

// Push adds a node to the heap
func (h *LFUHeap) Push(x interface{}) {
	h.mu.Lock()
	defer h.mu.Unlock()

	node := x.(*LFUNode)
	node.index = len(h.nodes)
	h.nodes = append(h.nodes, node)
	h.nodeMap[node.key] = node
}

// Pop removes and returns the minimum frequency node
func (h *LFUHeap) Pop() interface{} {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.nodes) == 0 {
		return nil
	}

	n := len(h.nodes)
	node := h.nodes[n-1]
	h.nodes[n-1] = nil // Avoid memory leak
	h.nodes = h.nodes[0 : n-1]

	delete(h.nodeMap, node.key)
	return node
}

// Peek returns the minimum frequency node without removing it
func (h *LFUHeap) Peek() *LFUNode {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.nodes) == 0 {
		return nil
	}
	return h.nodes[0]
}

// Get retrieves a node and increments its frequency
func (h *LFUHeap) Get(key string) (*LFUNode, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()

	node, exists := h.nodeMap[key]
	if !exists {
		return nil, false
	}

	// Increment frequency
	node.frequency++

	// Update min frequency if needed
	if node.frequency == h.minFreq+1 {
		h.updateMinFrequency()
	}

	// Re-heapify to maintain heap property
	heap.Fix(h, node.index)

	return node, true
}

// Put adds or updates a node
func (h *LFUHeap) Put(key string, value interface{}) *LFUNode {
	h.mu.Lock()
	defer h.mu.Unlock()

	if node, exists := h.nodeMap[key]; exists {
		// Update existing node
		node.value = value
		node.frequency++

		// Update min frequency if needed
		if node.frequency == h.minFreq+1 {
			h.updateMinFrequency()
		}

		heap.Fix(h, node.index)
		return node
	}

	// Create new node
	node := &LFUNode{
		key:       key,
		value:     value,
		frequency: 1,
	}

	heap.Push(h, node)

	// Update minimum frequency
	if h.minFreq > 1 {
		h.minFreq = 1
	}

	return node
}

// Remove removes a node from the heap
func (h *LFUHeap) Remove(key string) *LFUNode {
	h.mu.Lock()
	defer h.mu.Unlock()

	node, exists := h.nodeMap[key]
	if !exists {
		return nil
	}

	// Remove from heap
	heap.Remove(h, node.index)
	delete(h.nodeMap, key)

	// Update min frequency if this was the only node with min frequency
	if node.frequency == h.minFreq {
		h.updateMinFrequency()
	}

	return node
}

// RemoveLFU removes and returns the least frequently used node
func (h *LFUHeap) RemoveLFU() *LFUNode {
	h.mu.Lock()
	defer h.mu.Unlock()

	if len(h.nodes) == 0 {
		return nil
	}

	node := heap.Pop(h).(*LFUNode)

	// Update min frequency
	h.updateMinFrequency()

	return node
}

// Clear removes all nodes from the heap
func (h *LFUHeap) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.nodes = h.nodes[:0]
	h.nodeMap = make(map[string]*LFUNode)
	h.minFreq = 1
}

// updateMinFrequency updates the minimum frequency
func (h *LFUHeap) updateMinFrequency() {
	if len(h.nodes) == 0 {
		h.minFreq = 1
		return
	}

	minFreq := int64(^uint64(0) >> 1) // max int64
	for _, node := range h.nodes {
		if node.frequency < minFreq {
			minFreq = node.frequency
		}
	}
	h.minFreq = minFreq
}

// GetMinFrequency returns the current minimum frequency
func (h *LFUHeap) GetMinFrequency() int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.minFreq
}

// GetFrequency returns the frequency of a specific key
func (h *LFUHeap) GetFrequency(key string) int64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if node, exists := h.nodeMap[key]; exists {
		return node.frequency
	}
	return 0
}

// GetFrequencyDistribution returns frequency distribution statistics
func (h *LFUHeap) GetFrequencyDistribution() map[int64]int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	distribution := make(map[int64]int)
	for _, node := range h.nodes {
		distribution[node.frequency]++
	}
	return distribution
}

// GetTopK returns the k most frequently used keys
func (h *LFUHeap) GetTopK(k int) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if k <= 0 || len(h.nodes) == 0 {
		return nil
	}

	// Create a copy of nodes for sorting
	nodes := make([]*LFUNode, len(h.nodes))
	copy(nodes, h.nodes)

	// Sort by frequency (descending)
	for i := 0; i < len(nodes)-1; i++ {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[i].frequency < nodes[j].frequency {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}

	// Extract top k keys
	size := k
	if size > len(nodes) {
		size = len(nodes)
	}

	result := make([]string, size)
	for i := 0; i < size; i++ {
		result[i] = nodes[i].key
	}

	return result
}

// GetLeastK returns the k least frequently used keys
func (h *LFUHeap) GetLeastK(k int) []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if k <= 0 || len(h.nodes) == 0 {
		return nil
	}

	// Create a copy of nodes for sorting
	nodes := make([]*LFUNode, len(h.nodes))
	copy(nodes, h.nodes)

	// Sort by frequency (ascending)
	for i := 0; i < len(nodes)-1; i++ {
		for j := i + 1; j < len(nodes); j++ {
			if nodes[i].frequency > nodes[j].frequency {
				nodes[i], nodes[j] = nodes[j], nodes[i]
			}
		}
	}

	// Extract least k keys
	size := k
	if size > len(nodes) {
		size = len(nodes)
	}

	result := make([]string, size)
	for i := 0; i < size; i++ {
		result[i] = nodes[i].key
	}

	return result
}

// Contains checks if a key exists in the heap
func (h *LFUHeap) Contains(key string) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	_, exists := h.nodeMap[key]
	return exists
}

// GetAllKeys returns all keys in the heap
func (h *LFUHeap) GetAllKeys() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	keys := make([]string, 0, len(h.nodeMap))
	for key := range h.nodeMap {
		keys = append(keys, key)
	}
	return keys
}

// GetStats returns statistics about the LFU heap
func (h *LFUHeap) GetStats() LFUStats {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if len(h.nodes) == 0 {
		return LFUStats{
			Size:             0,
			MinFrequency:     0,
			MaxFrequency:     0,
			AverageFrequency: 0,
		}
	}

	minFreq := int64(^uint64(0) >> 1) // max int64
	maxFreq := int64(0)
	totalFreq := int64(0)

	for _, node := range h.nodes {
		if node.frequency < minFreq {
			minFreq = node.frequency
		}
		if node.frequency > maxFreq {
			maxFreq = node.frequency
		}
		totalFreq += node.frequency
	}

	return LFUStats{
		Size:             len(h.nodes),
		MinFrequency:     minFreq,
		MaxFrequency:     maxFreq,
		AverageFrequency: float64(totalFreq) / float64(len(h.nodes)),
		TotalFrequency:   totalFreq,
	}
}

// LFUStats contains statistics about the LFU heap
type LFUStats struct {
	Size             int     `json:"size"`
	MinFrequency     int64   `json:"min_frequency"`
	MaxFrequency     int64   `json:"max_frequency"`
	AverageFrequency float64 `json:"average_frequency"`
	TotalFrequency   int64   `json:"total_frequency"`
}

// Validate checks the heap invariant
func (h *LFUHeap) Validate() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Check heap property
	for i := 0; i < len(h.nodes); i++ {
		left := 2*i + 1
		right := 2*i + 2

		if left < len(h.nodes) {
			if h.nodes[i].frequency > h.nodes[left].frequency {
				return false
			}
		}

		if right < len(h.nodes) {
			if h.nodes[i].frequency > h.nodes[right].frequency {
				return false
			}
		}

		// Check index consistency
		if h.nodes[i].index != i {
			return false
		}

		// Check node map consistency
		if h.nodeMap[h.nodes[i].key] != h.nodes[i] {
			return false
		}
	}

	// Check node map size
	if len(h.nodeMap) != len(h.nodes) {
		return false
	}

	return true
}
