// Package storage provides unified object storage with support for multiple backends
// including local filesystem, S3, GCS, and Azure Blob Storage.
package storage

import (
	"context"
	"io"
	"time"
)

// Storage defines the unified storage interface.
type Storage interface {
	// Upload uploads an object
	Upload(ctx context.Context, key string, data io.Reader, opts ...UploadOption) error

	// Download downloads an object
	Download(ctx context.Context, key string) (io.ReadCloser, error)

	// Delete deletes an object
	Delete(ctx context.Context, key string) error

	// List lists objects with a prefix
	List(ctx context.Context, prefix string, opts ...ListOption) ([]Object, error)

	// Metadata retrieves object metadata
	Metadata(ctx context.Context, key string) (*ObjectMetadata, error)

	// Exists checks if an object exists
	Exists(ctx context.Context, key string) (bool, error)

	// Copy copies an object
	Copy(ctx context.Context, srcKey, dstKey string) error

	// Move moves an object
	Move(ctx context.Context, srcKey, dstKey string) error

	// PresignUpload generates a presigned URL for upload
	PresignUpload(ctx context.Context, key string, expiry time.Duration) (string, error)

	// PresignDownload generates a presigned URL for download
	PresignDownload(ctx context.Context, key string, expiry time.Duration) (string, error)
}

// Object represents a storage object.
type Object struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	LastModified time.Time         `json:"last_modified"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata"`
}

// ObjectMetadata represents object metadata.
type ObjectMetadata struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	LastModified time.Time         `json:"last_modified"`
	ETag         string            `json:"etag"`
	ContentType  string            `json:"content_type"`
	Metadata     map[string]string `json:"metadata"`
}

// UploadOption is a functional option for uploads.
type UploadOption func(*UploadOptions)

// ListOption is a functional option for listing.
type ListOption func(*ListOptions)

// UploadOptions contains upload options.
type UploadOptions struct {
	ContentType string
	Metadata    map[string]string
	ACL         string
}

// ListOptions contains list options.
type ListOptions struct {
	Limit     int
	Marker    string
	Recursive bool
}

// WithContentType sets the content type.
func WithContentType(contentType string) UploadOption {
	return func(o *UploadOptions) {
		o.ContentType = contentType
	}
}

// WithMetadata sets metadata.
func WithMetadata(metadata map[string]string) UploadOption {
	return func(o *UploadOptions) {
		o.Metadata = metadata
	}
}

// WithACL sets ACL.
func WithACL(acl string) UploadOption {
	return func(o *UploadOptions) {
		o.ACL = acl
	}
}

// WithLimit sets the limit.
func WithLimit(limit int) ListOption {
	return func(o *ListOptions) {
		o.Limit = limit
	}
}

// WithMarker sets the marker.
func WithMarker(marker string) ListOption {
	return func(o *ListOptions) {
		o.Marker = marker
	}
}

// WithRecursive sets recursive listing.
func WithRecursive(recursive bool) ListOption {
	return func(o *ListOptions) {
		o.Recursive = recursive
	}
}

// applyUploadOptions applies upload options.
func applyUploadOptions(opts ...UploadOption) *UploadOptions {
	options := &UploadOptions{
		ContentType: "application/octet-stream",
		Metadata:    make(map[string]string),
		ACL:         "private",
	}
	for _, opt := range opts {
		opt(options)
	}

	return options
}

// applyListOptions applies list options.
func applyListOptions(opts ...ListOption) *ListOptions {
	options := &ListOptions{
		Limit:     1000,
		Recursive: false,
	}
	for _, opt := range opts {
		opt(options)
	}

	return options
}
