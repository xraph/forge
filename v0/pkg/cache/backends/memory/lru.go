package memory

import (
	"sync"
)

// LRUList implements a doubly-linked list for LRU cache eviction
type LRUList struct {
	head   *CacheEntry
	tail   *CacheEntry
	length int
	mu     sync.RWMutex
}

// NewLRUList creates a new LRU list
func NewLRUList() *LRUList {
	list := &LRUList{}
	// Create sentinel nodes
	list.head = &CacheEntry{}
	list.tail = &CacheEntry{}
	list.head.next = list.tail
	list.tail.prev = list.head
	return list
}

// PushFront adds an entry to the front of the list (most recently used)
func (l *LRUList) PushFront(entry *CacheEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.insertAfter(l.head, entry)
	l.length++
}

// MoveToFront moves an existing entry to the front of the list
func (l *LRUList) MoveToFront(entry *CacheEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.prev == nil || entry.next == nil {
		// Entry not in list, add it
		l.insertAfter(l.head, entry)
		l.length++
	} else {
		// Remove from current position
		l.remove(entry)
		// Insert at front
		l.insertAfter(l.head, entry)
	}
}

// Remove removes an entry from the list
func (l *LRUList) Remove(entry *CacheEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if entry.prev != nil && entry.next != nil {
		l.remove(entry)
		l.length--
	}
}

// Back returns the least recently used entry (back of the list)
func (l *LRUList) Back() *CacheEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.tail.prev == l.head {
		return nil // Empty list
	}
	return l.tail.prev
}

// Front returns the most recently used entry (front of the list)
func (l *LRUList) Front() *CacheEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.head.next == l.tail {
		return nil // Empty list
	}
	return l.head.next
}

// Length returns the number of entries in the list
func (l *LRUList) Length() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.length
}

// IsEmpty returns true if the list is empty
func (l *LRUList) IsEmpty() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.length == 0
}

// Clear removes all entries from the list
func (l *LRUList) Clear() {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.head.next = l.tail
	l.tail.prev = l.head
	l.length = 0
}

// PopBack removes and returns the least recently used entry
func (l *LRUList) PopBack() *CacheEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.tail.prev == l.head {
		return nil // Empty list
	}

	entry := l.tail.prev
	l.remove(entry)
	l.length--
	return entry
}

// PopFront removes and returns the most recently used entry
func (l *LRUList) PopFront() *CacheEntry {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.head.next == l.tail {
		return nil // Empty list
	}

	entry := l.head.next
	l.remove(entry)
	l.length--
	return entry
}

// ForEach iterates over all entries in the list from front to back
func (l *LRUList) ForEach(fn func(*CacheEntry) bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	current := l.head.next
	for current != l.tail {
		if !fn(current) {
			break
		}
		current = current.next
	}
}

// ForEachReverse iterates over all entries in the list from back to front
func (l *LRUList) ForEachReverse(fn func(*CacheEntry) bool) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	current := l.tail.prev
	for current != l.head {
		if !fn(current) {
			break
		}
		current = current.prev
	}
}

// ToSlice returns all entries as a slice (front to back)
func (l *LRUList) ToSlice() []*CacheEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	entries := make([]*CacheEntry, 0, l.length)
	current := l.head.next
	for current != l.tail {
		entries = append(entries, current)
		current = current.next
	}
	return entries
}

// insertAfter inserts an entry after the given node
func (l *LRUList) insertAfter(node, entry *CacheEntry) {
	entry.prev = node
	entry.next = node.next
	node.next.prev = entry
	node.next = entry
}

// remove removes an entry from its current position
func (l *LRUList) remove(entry *CacheEntry) {
	entry.prev.next = entry.next
	entry.next.prev = entry.prev
	entry.prev = nil
	entry.next = nil
}

// Validate checks the consistency of the list (for debugging)
func (l *LRUList) Validate() bool {
	l.mu.RLock()
	defer l.mu.RUnlock()

	if l.head.prev != nil || l.tail.next != nil {
		return false
	}

	count := 0
	current := l.head.next
	for current != l.tail {
		if current.prev == nil || current.next == nil {
			return false
		}
		if current.prev.next != current || current.next.prev != current {
			return false
		}
		count++
		current = current.next
	}

	return count == l.length
}
