// Package server provides the HTTP API for async HAR capture operations.
//
// Endpoints:
//
//	POST /captures        — enqueue a new capture; returns operation ID immediately
//	GET  /captures/{id}   — poll operation status and retrieve artefact URLs
package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/tomasbasham/har-capture/internal/capture"
	"github.com/tomasbasham/har-capture/internal/operation"
	"github.com/tomasbasham/har-capture/internal/storage"
)

// Server holds the dependencies shared across HTTP handlers.
type Server struct {
	store    operation.Store
	uploader storage.Uploader
	mux      *http.ServeMux

	// defaultCaptureOptions are used as a base for every capture; request
	// fields may override individual values.
	defaultCaptureOptions capture.Options
}

// New creates a Server wired to the given store and uploader.
func New(store operation.Store, uploader storage.Uploader, defaults capture.Options) *Server {
	s := &Server{
		store:                 store,
		uploader:              uploader,
		defaultCaptureOptions: defaults,
	}

	s.mux = http.NewServeMux()
	s.mux.HandleFunc("POST /captures", s.handleCreateCapture)
	s.mux.HandleFunc("GET /captures/{id}", s.handleGetCapture)

	return s
}

// ListenAndServe starts the HTTP server on the given address.
func (s *Server) ListenAndServe(addr string) error {
	srv := &http.Server{
		Addr:         addr,
		Handler:      s.mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	return srv.ListenAndServe()
}

// createCaptureRequest is the JSON body for POST /captures.
type createCaptureRequest struct {
	URL               string `json:"url"`
	NavigationTimeout string `json:"navigation_timeout,omitempty"`
	TotalTimeout      string `json:"total_timeout,omitempty"`
	Screenshots       bool   `json:"screenshots"`
}

// createCaptureResponse is returned immediately from POST /captures.
type createCaptureResponse struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
}

func (s *Server) handleCreateCapture(w http.ResponseWriter, r *http.Request) {
	var req createCaptureRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}
	if req.URL == "" {
		writeError(w, http.StatusBadRequest, "url is required")
		return
	}

	opts := s.defaultCaptureOptions
	opts.URL = req.URL
	opts.Screenshots = req.Screenshots

	if req.NavigationTimeout != "" {
		d, err := time.ParseDuration(req.NavigationTimeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid navigation_timeout %q: %s", req.NavigationTimeout, err))
			return
		}
		opts.NavigationTimeout = d
	}
	if req.TotalTimeout != "" {
		d, err := time.ParseDuration(req.TotalTimeout)
		if err != nil {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid total_timeout %q: %s", req.TotalTimeout, err))
			return
		}
		opts.TotalTimeout = d
	}

	op, err := s.store.Create(req.URL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create operation: "+err.Error())
		return
	}

	// Run the capture in the background. The request context is intentionally
	// not used here — we do not want the capture to be cancelled when the HTTP
	// connection closes.
	go operation.Run(r.Context(), operation.WorkerOptions{
		OperationID:    op.ID,
		Store:          s.store,
		Uploader:       s.uploader,
		CaptureOptions: opts,
	})

	writeJSON(w, http.StatusAccepted, createCaptureResponse{
		OperationID: op.ID,
		Status:      string(operation.StatusPending),
	})
}

func (s *Server) handleGetCapture(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "operation id is required")
		return
	}

	op, err := s.store.Get(id)
	if err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("operation %q not found", id))
		return
	}

	writeJSON(w, http.StatusOK, op)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
