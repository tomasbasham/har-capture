package capture

import (
	"sync"
	"time"

	"github.com/chromedp/cdproto/network"
)

// pendingRequest holds the request side of a network event whilst we await
// the corresponding response. RequestID is the correlation key.
type pendingRequest struct {
	requestID    network.RequestID
	method       string
	url          string
	headers      network.Headers
	wallTime     time.Time
	resourceType network.ResourceType
	pageRef      string
}

// completedEntry holds a fully correlated request+response pair ready for
// HAR assembly.
type completedEntry struct {
	request  pendingRequest
	response *network.EventResponseReceived
}

// requestStore correlates requests and responses by RequestID in a
// concurrency-safe manner.
type requestStore struct {
	mu      sync.Mutex
	pending map[network.RequestID]pendingRequest
}

func newRequestStore() *requestStore {
	return &requestStore{
		pending: make(map[network.RequestID]pendingRequest),
	}
}

func (s *requestStore) addRequest(r pendingRequest) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending[r.requestID] = r
}

// correlate attempts to pair a response event with its pending request.
// Returns the completed entry and true if found, otherwise false.
func (s *requestStore) correlate(ev *network.EventResponseReceived) (completedEntry, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.pending[ev.RequestID]
	if !ok {
		return completedEntry{}, false
	}

	delete(s.pending, ev.RequestID)

	return completedEntry{request: req, response: ev}, true
}
