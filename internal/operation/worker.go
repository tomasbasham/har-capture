package operation

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/tomasbasham/har-capture/internal/capture"
	"github.com/tomasbasham/har-capture/internal/storage"
)

// WorkerOptions configures a capture worker invocation.
type WorkerOptions struct {
	CaptureOptions capture.Options
	OperationID    string
	Store          Store
	Uploader       storage.Uploader
}

// Run executes a capture, uploads the resulting artefacts to GCS, and
// transitions the operation through running → complete | failed.
//
// Run is intended to be called in a separate goroutine; it owns the full
// lifecycle of the operation from the moment it is called.
func Run(ctx context.Context, opts WorkerOptions) {
	if err := opts.Store.MarkRunning(opts.OperationID); err != nil {
		// If we cannot even mark it running the store is broken; nothing to do.
		return
	}

	result, err := capture.Capture(ctx, opts.CaptureOptions)
	if err != nil {
		_ = opts.Store.MarkFailed(opts.OperationID, fmt.Errorf("capture: %w", err))
		return
	}

	artefacts, err := uploadArtefacts(ctx, opts.OperationID, result, opts.Uploader)
	if err != nil {
		_ = opts.Store.MarkFailed(opts.OperationID, fmt.Errorf("upload: %w", err))
		return
	}

	_ = opts.Store.MarkComplete(opts.OperationID, result.TTFB, result.TimedOut, artefacts)
}

// uploadArtefacts serialises the HAR and any screenshots and uploads them to
// GCS. Returns the artefact list ready to be stored on the operation.
func uploadArtefacts(ctx context.Context, operationID string, result *capture.Result, uploader storage.Uploader) ([]Artefact, error) {
	var artefacts []Artefact

	// Upload HAR.
	harJSON, err := json.Marshal(result.HAR)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal HAR: %w", err)
	}

	harRequest := &storage.UploadRequest{
		ObjectName:  objectPath(operationID, "capture.har"),
		Content:     bytes.NewReader(harJSON),
		ContentType: "application/json",
	}

	uploaded, err := uploader.Upload(ctx, harRequest)
	if err != nil {
		return nil, err
	}
	artefacts = append(artefacts, Artefact{
		Name:      "har",
		SignedURL: uploaded.SignedURL,
		ExpiresAt: uploaded.ExpiresAt,
	})

	// Upload screenshots.
	for i, s := range result.Screenshots {
		name := fmt.Sprintf("screenshot_%02d_%s.png", i+1, s.Stage)

		screenshotRequest := &storage.UploadRequest{
			ObjectName:  objectPath(operationID, name),
			Content:     bytes.NewReader(s.PNG),
			ContentType: "image/png",
		}

		uploaded, err := uploader.Upload(ctx, screenshotRequest)
		if err != nil {
			return nil, fmt.Errorf("screenshot %d: %w", i+1, err)
		}
		artefacts = append(artefacts, Artefact{
			Name:      fmt.Sprintf("screenshot_%s", s.Stage),
			SignedURL: uploaded.SignedURL,
			ExpiresAt: uploaded.ExpiresAt,
		})
	}

	return artefacts, nil
}

func objectPath(operationID, filename string) string {
	date := time.Now().UTC().Format("2006/01/02")
	return fmt.Sprintf("operations/%s/%s/%s", date, operationID, filename)
}
