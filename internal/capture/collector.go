package capture

import (
	"context"

	"github.com/chromedp/cdproto/har"
)

// collector accumulates network events emitted by the CDP listener and drains
// them into typed slices. It owns the channels that would otherwise need to be
// passed individually into a drain loop, removing the need for a labelled
// break in Capture.
//
// Typical usage:
//
//	coll := newCollector()
//	chromedp.ListenTarget(ctx, func(ev any) {
//	    // forward events via coll.send and coll.markDone
//	})
//	pages, entries, timedOut := coll.wait(totalCtx)
type collector struct {
	resultCh chan any
	doneCh   chan struct{}
	doneOnce *onceCloser
}

func newCollector() *collector {
	doneCh := make(chan struct{})
	return &collector{
		resultCh: make(chan any, 64),
		doneCh:   doneCh,
		doneOnce: &onceCloser{ch: doneCh},
	}
}

// send delivers an event into the collector. Safe to call from the CDP
// listener goroutine.
func (c *collector) send(v any) {
	c.resultCh <- v
}

// markDone signals that the page has reached networkIdle. Idempotent.
func (c *collector) markDone() {
	c.doneOnce.close()
}

// wait blocks until either networkIdle is signalled via markDone or ctx is
// cancelled, then drains any remaining buffered events and returns the
// collected slices. A context cancellation is treated as a graceful cutoff —
// timedOut will be true but the collected data is still returned.
func (c *collector) wait(ctx context.Context) (pages []har.Page, entries []completedEntry, timedOut bool) {
	select {
	case <-c.doneCh:
	case <-ctx.Done():
		timedOut = true
	}

	for len(c.resultCh) > 0 {
		c.accumulate(<-c.resultCh, &pages, &entries)
	}

	return pages, entries, timedOut
}

func (c *collector) accumulate(data any, pages *[]har.Page, entries *[]completedEntry) {
	switch d := data.(type) {
	case har.Page:
		*pages = append(*pages, d)
	case completedEntry:
		*entries = append(*entries, d)
	}
}
