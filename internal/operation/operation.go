// Package operation provides the domain model for async capture operations.  An
// Operation moves through a linear lifecycle:
//
//	pending → running → complete | failed.
//
// The store is the authoritative source of truth for operation state; HTTP
// handlers read and write exclusively through it.
package operation

import (
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Status represents the lifecycle state of an operation.
type Status string

const (
	StatusPending  Status = "pending"
	StatusRunning  Status = "running"
	StatusComplete Status = "complete"
	StatusFailed   Status = "failed"
)

// Artefact is a named output produced by a completed operation, referenced by
// a signed URL valid for a bounded period.
type Artefact struct {
	Name      string    `json:"name"`
	SignedURL string    `json:"signed_url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Operation represents a single async capture job.
type Operation struct {
	ID        string    `json:"id"`
	Status    Status    `json:"status"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// TTFB is populated once the operation reaches StatusComplete.
	TTFB time.Duration `json:"ttfb_ms"`

	// TimedOut is true if the capture was cut off before networkIdle.
	TimedOut bool `json:"timed_out"`

	// Artefacts lists the GCS objects produced by a completed operation.
	// Empty until the operation reaches StatusComplete.
	Artefacts []Artefact `json:"artefacts,omitempty"`

	// Error is non-empty if the operation reached StatusFailed.
	Error string `json:"error,omitempty"`
}

// Store is the interface for persisting and retrieving operations. The
// in-memory implementation below is suitable for a single instance; a Firestore
// or Cloud SQL-backed implementation would satisfy the same interface for
// multi-instance deployments.
type Store interface {
	Create(url string) (*Operation, error)
	Get(id string) (*Operation, error)
	MarkRunning(id string) error
	MarkComplete(id string, ttfb time.Duration, timedOut bool, artefacts []Artefact) error
	MarkFailed(id string, err error) error
}

// MemoryStore is a concurrency-safe in-memory Store implementation.
type MemoryStore struct {
	mu  sync.RWMutex
	ops map[string]*Operation
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{ops: make(map[string]*Operation)}
}

func (s *MemoryStore) Create(url string) (*Operation, error) {
	op := &Operation{
		ID:        uuid.New().String(),
		Status:    StatusPending,
		URL:       url,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	s.mu.Lock()
	s.ops[op.ID] = op
	s.mu.Unlock()

	return op, nil
}

func (s *MemoryStore) Get(id string) (*Operation, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	op, ok := s.ops[id]
	if !ok {
		return nil, fmt.Errorf("operation %q not found", id)
	}
	// Return a copy to prevent callers from mutating internal state.
	copy := *op
	return &copy, nil
}

func (s *MemoryStore) MarkRunning(id string) error {
	return s.update(id, func(op *Operation) {
		op.Status = StatusRunning
	})
}

func (s *MemoryStore) MarkComplete(id string, ttfb time.Duration, timedOut bool, artefacts []Artefact) error {
	return s.update(id, func(op *Operation) {
		op.Status = StatusComplete
		op.TTFB = ttfb
		op.TimedOut = timedOut
		op.Artefacts = artefacts
	})
}

func (s *MemoryStore) MarkFailed(id string, err error) error {
	return s.update(id, func(op *Operation) {
		op.Status = StatusFailed
		op.Error = err.Error()
	})
}

func (s *MemoryStore) update(id string, fn func(*Operation)) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	op, ok := s.ops[id]
	if !ok {
		return fmt.Errorf("operation %q not found", id)
	}
	fn(op)
	op.UpdatedAt = time.Now()
	return nil
}
