package storage

import "errors"

var (
	// Configuration errors
	ErrNoBackendsConfigured   = errors.New("no storage backends configured")
	ErrNoDefaultBackend       = errors.New("no default backend specified")
	ErrDefaultBackendNotFound = errors.New("default backend not found in configuration")
	ErrInvalidBackendType     = errors.New("invalid backend type")
	ErrBackendNotFound        = errors.New("backend not found")

	// Operation errors
	ErrObjectNotFound        = errors.New("object not found")
	ErrObjectAlreadyExists   = errors.New("object already exists")
	ErrInvalidKey            = errors.New("invalid object key")
	ErrUploadFailed          = errors.New("upload failed")
	ErrDownloadFailed        = errors.New("download failed")
	ErrDeleteFailed          = errors.New("delete failed")
	ErrPresignNotSupported   = errors.New("presigned URLs not supported for this backend")
	ErrMultipartNotSupported = errors.New("multipart upload not supported for this backend")
)
