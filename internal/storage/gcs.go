// Package storage provides an abstraction for uploading capture artefacts and
// generating time-limited signed URLs for retrieval. The GCS implementation is
// the production backend; the interface allows alternative implementations for
// testing.
package storage

import (
	"context"
	"fmt"
	"io"
	"time"

	"cloud.google.com/go/storage"
	"google.golang.org/api/option"
)

const signedURLTTL = 1 * time.Hour

// GCSUploader uploads objects to a Google Cloud Storage bucket.
type GCSUploader struct {
	client *storage.Client
	bucket string
}

// NewGCSUploader creates a GCSUploader for the given bucket. opts are passed
// through to the underlying GCS client, allowing credential injection.
func NewGCSUploader(ctx context.Context, bucket string, opts ...option.ClientOption) (*GCSUploader, error) {
	client, err := storage.NewClient(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create GCS client: %w", err)
	}
	return &GCSUploader{client: client, bucket: bucket}, nil
}

// Upload writes content to GCS at objectName and returns a signed URL.
func (u *GCSUploader) Upload(ctx context.Context, req *UploadRequest) (*UploadResult, error) {
	obj := u.client.Bucket(u.bucket).Object(req.ObjectName)
	w := obj.NewWriter(ctx)
	w.ContentType = req.ContentType

	if _, err := io.Copy(w, req.Content); err != nil {
		_ = w.Close()
		return nil, fmt.Errorf("storage: upload write failed for %q: %w", req.ObjectName, err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("storage: upload close failed for %q: %w", req.ObjectName, err)
	}

	expiresAt := time.Now().Add(signedURLTTL)
	signedURL, err := u.client.Bucket(u.bucket).SignedURL(req.ObjectName, &storage.SignedURLOptions{
		Method:  "GET",
		Expires: expiresAt,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: failed to sign URL for %q: %w", req.ObjectName, err)
	}

	return &UploadResult{
		ObjectName: req.ObjectName,
		SignedURL:  signedURL,
		ExpiresAt:  expiresAt,
	}, nil
}
