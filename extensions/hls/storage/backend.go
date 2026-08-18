package storage

import (
	"context"
	"io"
	"time"
)

// Backend is the object storage HLS needs, and nothing more.
//
// It is deliberately narrower than any storage library's own surface: six
// operations over a flat key space, which is all HLSStorage has ever called.
// Owning the interface here rather than typing against a vendor's is what
// makes the backing library replaceable -- swapping it is a new file next to
// this one instead of an edit at every call site. That is not hypothetical;
// this package was written against the forge storage extension and moved to
// trove without HLSStorage changing at all.
type Backend interface {
	// Put stores data at key. An empty contentType leaves it to the backend.
	Put(ctx context.Context, key string, r io.Reader, contentType string) error

	// Get opens key for reading. The caller closes the reader.
	Get(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete removes key. Removing a key that is not there is not an error,
	// because both callers of it are cleanup paths that would otherwise have
	// to race the thing they are deleting.
	Delete(ctx context.Context, key string) error

	// Exists reports whether key is present. A missing key is (false, nil),
	// never an error.
	Exists(ctx context.Context, key string) (bool, error)

	// List returns every object under prefix, recursively.
	List(ctx context.Context, prefix string) ([]Object, error)

	// Health reports whether the backend is reachable and usable.
	Health(ctx context.Context) error
}

// Object is one stored item, reduced to the fields HLS reads.
type Object struct {
	Key          string
	Size         int64
	LastModified time.Time
}
