package storage

import (
	"context"
	"errors"
	"io"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
)

// TroveBackend implements Backend over a Trove instance.
//
// Trove addresses objects as bucket plus key; Backend is a flat key space.
// The bucket is fixed at construction and every key passes through unchanged,
// so the key layout HLSStorage produces is whatever it was before this type
// existed. That matters: existing content must stay addressable.
type TroveBackend struct {
	trove  *trove.Trove
	bucket string
}

// NewTroveBackend binds a Trove instance to one bucket.
func NewTroveBackend(t *trove.Trove, bucket string) *TroveBackend {
	if bucket == "" {
		bucket = DefaultBucket
	}
	return &TroveBackend{trove: t, bucket: bucket}
}

// DefaultBucket is where HLS content lands when no bucket is configured.
const DefaultBucket = "hls"

// EnsureBucket creates the backing bucket if it is not already there.
//
// Trove will not write into a bucket that does not exist, and the storage
// extension this replaced had no bucket concept at all, so nothing in HLS ever
// created one. Call this once at startup rather than lazily on write: a
// create-on-demand in Put would run on every segment and would race itself
// across the nodes of a distributed stream.
func (b *TroveBackend) EnsureBucket(ctx context.Context) error {
	err := b.trove.CreateBucket(ctx, b.bucket)
	if errors.Is(err, driver.ErrBucketExists) {
		return nil
	}

	return err
}

func (b *TroveBackend) Put(ctx context.Context, key string, r io.Reader, contentType string) error {
	var opts []driver.PutOption
	if contentType != "" {
		opts = append(opts, driver.WithContentType(contentType))
	}

	_, err := b.trove.Put(ctx, b.bucket, key, r, opts...)
	return err
}

func (b *TroveBackend) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := b.trove.Get(ctx, b.bucket, key)
	if err != nil {
		return nil, err
	}

	// ObjectReader embeds io.ReadCloser and carries metadata the Backend
	// contract does not expose. Returning the embedded reader keeps callers
	// from having to know that.
	return obj, nil
}

func (b *TroveBackend) Delete(ctx context.Context, key string) error {
	err := b.trove.Delete(ctx, b.bucket, key)

	// Backend documents deleting an absent key as a success. Both callers are
	// cleanup loops that would otherwise have to race whatever they are
	// removing.
	if errors.Is(err, driver.ErrNotFound) {
		return nil
	}

	return err
}

func (b *TroveBackend) Exists(ctx context.Context, key string) (bool, error) {
	_, err := b.trove.Head(ctx, b.bucket, key)
	if err != nil {
		// ErrObjectNotFound unwraps to ErrNotFound, and a missing bucket means
		// the object is missing too, so the parent sentinel is the right match.
		if errors.Is(err, driver.ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

func (b *TroveBackend) List(ctx context.Context, prefix string) ([]Object, error) {
	iter, err := b.trove.List(ctx, b.bucket, driver.WithPrefix(prefix))
	if err != nil {
		// A bucket that was never written to lists as empty rather than as a
		// failure; callers use List to decide what to clean up, and an error
		// there would abort cleanup of everything else.
		if errors.Is(err, driver.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	defer iter.Close()

	infos, err := iter.All(ctx)
	if err != nil {
		return nil, err
	}

	objects := make([]Object, 0, len(infos))
	for _, info := range infos {
		objects = append(objects, Object{
			Key:          info.Key,
			Size:         info.Size,
			LastModified: info.LastModified,
		})
	}

	return objects, nil
}

func (b *TroveBackend) Health(ctx context.Context) error {
	return b.trove.Health(ctx)
}
