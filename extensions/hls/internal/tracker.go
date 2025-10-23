package internal

import (
	"sync"
	"time"
)

// SegmentTracker tracks segments for live streams
type SegmentTracker struct {
	streamID string
	segments map[string]*VariantSegments // variantID -> segments
	mu       sync.RWMutex
}

// VariantSegments tracks segments for a single variant
type VariantSegments struct {
	Segments      []*SegmentEntry
	MediaSequence int
	LastSegment   int
}

// SegmentEntry represents a tracked segment
type SegmentEntry struct {
	Index     int
	Duration  float64
	Size      int64
	Timestamp time.Time
	Available bool
}

// NewSegmentTracker creates a new segment tracker
func NewSegmentTracker(streamID string) *SegmentTracker {
	return &SegmentTracker{
		streamID: streamID,
		segments: make(map[string]*VariantSegments),
	}
}

// AddSegment adds a segment for a variant
func (t *SegmentTracker) AddSegment(variantID string, index int, duration float64, size int64) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, exists := t.segments[variantID]; !exists {
		t.segments[variantID] = &VariantSegments{
			Segments:      make([]*SegmentEntry, 0),
			MediaSequence: 0,
		}
	}

	entry := &SegmentEntry{
		Index:     index,
		Duration:  duration,
		Size:      size,
		Timestamp: time.Now(),
		Available: true,
	}

	t.segments[variantID].Segments = append(t.segments[variantID].Segments, entry)
	t.segments[variantID].LastSegment = index
}

// GetSegments returns segments for a variant
func (t *SegmentTracker) GetSegments(variantID string) []*SegmentEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if vs, exists := t.segments[variantID]; exists {
		// Return a copy
		result := make([]*SegmentEntry, len(vs.Segments))
		copy(result, vs.Segments)
		return result
	}

	return nil
}

// GetSegmentsWindow returns the last N segments (DVR window)
func (t *SegmentTracker) GetSegmentsWindow(variantID string, windowSize int) []*SegmentEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if vs, exists := t.segments[variantID]; exists {
		if len(vs.Segments) <= windowSize {
			result := make([]*SegmentEntry, len(vs.Segments))
			copy(result, vs.Segments)
			return result
		}

		start := len(vs.Segments) - windowSize
		result := make([]*SegmentEntry, windowSize)
		copy(result, vs.Segments[start:])
		return result
	}

	return nil
}

// GetMediaSequence returns the media sequence number for a variant
func (t *SegmentTracker) GetMediaSequence(variantID string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if vs, exists := t.segments[variantID]; exists {
		return vs.MediaSequence
	}

	return 0
}

// GetLastSegment returns the last segment index for a variant
func (t *SegmentTracker) GetLastSegment(variantID string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if vs, exists := t.segments[variantID]; exists {
		return vs.LastSegment
	}

	return -1
}

// RemoveOldSegments removes segments older than the window size
func (t *SegmentTracker) RemoveOldSegments(variantID string, keepLast int) []int {
	t.mu.Lock()
	defer t.mu.Unlock()

	if vs, exists := t.segments[variantID]; exists {
		if len(vs.Segments) > keepLast {
			toRemove := len(vs.Segments) - keepLast
			removed := make([]int, toRemove)

			for i := 0; i < toRemove; i++ {
				removed[i] = vs.Segments[i].Index
			}

			vs.Segments = vs.Segments[toRemove:]
			vs.MediaSequence += toRemove

			return removed
		}
	}

	return nil
}

// Clear clears all segments for a variant
func (t *SegmentTracker) Clear(variantID string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	delete(t.segments, variantID)
}

// ClearAll clears all segments for all variants
func (t *SegmentTracker) ClearAll() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.segments = make(map[string]*VariantSegments)
}

// GetVariants returns all variant IDs
func (t *SegmentTracker) GetVariants() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()

	variants := make([]string, 0, len(t.segments))
	for variantID := range t.segments {
		variants = append(variants, variantID)
	}

	return variants
}

// GetSegmentCount returns the total number of segments for a variant
func (t *SegmentTracker) GetSegmentCount(variantID string) int {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if vs, exists := t.segments[variantID]; exists {
		return len(vs.Segments)
	}

	return 0
}
