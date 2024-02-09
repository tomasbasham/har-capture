package storage

import (
	"context"
	"io"
	"time"
)

// Uploader persists artefacts to a storage backend and returns signed URLs.
type Uploader interface {
	Upload(ctx context.Context, req *UploadRequest) (*UploadResult, error)
}

type UploadRequest struct {
	// ObjectName is the GCS object path within the configured bucket.
	ObjectName string

	// Content is the data to be uploaded.
	Content io.Reader

	// ContentType is the MIME type of the content, e.g. "application/json".
	ContentType string
}

// UploadResult is the outcome of a successful upload.
type UploadResult struct {
	// ObjectName is the GCS object path within the configured bucket.
	ObjectName string

	// SignedURL provides time-limited access to the object.
	SignedURL string

	// ExpiresAt is when the signed URL becomes invalid.
	ExpiresAt time.Time
}
