package storage

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// LocalUploader writes artefacts to a directory on the local filesystem. The
// signed URL returned is a file:// URL - there is no expiry concept for local
// files, so ExpiresAt is set to the zero value.
type LocalUploader struct {
	baseDir string
}

// NewLocalUploader creates a LocalUploader that writes artefacts under
// baseDir. The directory is created if it does not already exist.
func NewLocalUploader(baseDir string) (*LocalUploader, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("storage: failed to create local base directory %q: %w", baseDir, err)
	}
	abs, err := filepath.Abs(baseDir)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to resolve absolute path for %q: %w", baseDir, err)
	}
	return &LocalUploader{baseDir: abs}, nil
}

// Upload writes content to baseDir/objectName, creating any intermediate
// directories as needed. The returned SignedURL is a file:// URL pointing to
// the written file.
func (u *LocalUploader) Upload(_ context.Context, req *UploadRequest) (*UploadResult, error) {
	dest := filepath.Join(u.baseDir, filepath.FromSlash(req.ObjectName))

	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return nil, fmt.Errorf("storage: failed to create directory for %q: %w", req.ObjectName, err)
	}

	f, err := os.Create(dest)
	if err != nil {
		return nil, fmt.Errorf("storage: failed to create file %q: %w", dest, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, req.Content); err != nil {
		return nil, fmt.Errorf("storage: failed to write file %q: %w", dest, err)
	}

	fileURL := &url.URL{Scheme: "file", Path: filepath.ToSlash(dest)}

	return &UploadResult{
		ObjectName: req.ObjectName,
		SignedURL:  fileURL.String(),
		ExpiresAt:  time.Time{},
	}, nil
}
