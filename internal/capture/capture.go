// Package capture provides a HAR (HTTP Archive) capturer built on top of the
// Chrome DevTools Protocol (CDP). It is transport-agnostic: callers receive a
// har.HAR value and may serialise or forward it however they choose.
package capture

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/chromedp/cdproto/har"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

// LifecycleStage identifies a named point in the page loading process at
// which a screenshot was taken.
type LifecycleStage string

const (
	StageDocumentLoad         LifecycleStage = "load"
	StageFirstContentfulPaint LifecycleStage = "firstContentfulPaint"
	StageNetworkIdle          LifecycleStage = "networkIdle"
)

// lifecycleOrder defines the canonical ordering of stages for sorting
// screenshots that may have been collected concurrently.
var lifecycleOrder = map[LifecycleStage]int{
	StageDocumentLoad:         0,
	StageFirstContentfulPaint: 1,
	StageNetworkIdle:          2,
}

// Screenshot holds a PNG image captured at a particular lifecycle stage.
type Screenshot struct {
	Stage      LifecycleStage
	CapturedAt time.Time
	PNG        []byte
}

// Options controls the behaviour of a capture run.
type Options struct {
	// URL is the page to capture. Required.
	URL string

	// NavigationTimeout is the maximum duration to wait for the initial page
	// navigation to complete. When it elapses, collection continues with
	// whatever has been received so far — a navigation timeout is not fatal.
	// Defaults to 10 seconds if zero.
	NavigationTimeout time.Duration

	// TotalTimeout is the maximum duration for the entire capture, measured
	// from the moment Capture is called. This bounds both navigation and the
	// subsequent wait for networkIdle. Defaults to 30 seconds if zero.
	TotalTimeout time.Duration

	// BrowserVersion is embedded into the HAR creator metadata. When empty,
	// the string "unknown" is used. In practice you would retrieve this via
	// the Browser.getVersion CDP command.
	BrowserVersion string

	// Screenshots controls whether PNG screenshots are captured at each
	// lifecycle stage (load, firstContentfulPaint, networkIdle).
	Screenshots bool

	// ViewportWidth and ViewportHeight set the browser viewport dimensions.
	// Defaults to 1920x1080 if either is zero.
	ViewportWidth  int64
	ViewportHeight int64
}

// Result is the outcome of a capture run.
type Result struct {
	HAR har.HAR

	// TTFB is the time-to-first-byte for the document request — the duration
	// between the request being sent and the first response byte being received.
	// Zero if the document response was not observed.
	TTFB time.Duration

	// Screenshots contains PNG images captured at lifecycle stages, in
	// lifecycle order. Empty if Options.Screenshots was false.
	Screenshots []Screenshot

	// TimedOut is true when the capture was cut off by TotalTimeout rather
	// than by a networkIdle event. The HAR contains whatever was collected up
	// to that point; no entries are discarded.
	TimedOut bool
}

// Capture navigates to the URL specified in opts, records all network
// activity until the page reaches networkIdle or TotalTimeout elapses, and
// returns a Result containing the assembled HAR.
//
// Capture is safe to call concurrently; each call creates an isolated browser
// context.
func Capture(ctx context.Context, opts Options) (*Result, error) {
	if opts.URL == "" {
		return nil, fmt.Errorf("capture: URL must not be empty")
	}

	navTimeout := opts.NavigationTimeout
	if navTimeout == 0 {
		navTimeout = 10 * time.Second
	}

	totalTimeout := opts.TotalTimeout
	if totalTimeout == 0 {
		totalTimeout = 30 * time.Second
	}

	browserVersion := opts.BrowserVersion
	if browserVersion == "" {
		browserVersion = "unknown"
	}

	viewportWidth := opts.ViewportWidth
	viewportHeight := opts.ViewportHeight
	if viewportWidth == 0 || viewportHeight == 0 {
		viewportWidth = 1920
		viewportHeight = 1080
	}

	// totalCtx bounds the entire capture including browser startup.
	totalCtx, cancelTotal := context.WithTimeout(ctx, totalTimeout)
	defer cancelTotal()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(totalCtx,
		append(
			chromedp.DefaultExecAllocatorOptions[:],
			chromedp.Flag("headless", true),
		)...,
	)
	defer cancelAlloc()

	// Provide no-op log funcs to suppress chromedp's internal error output for
	// CDP events it cannot unmarshal — these arise from version skew between
	// the installed Chrome binary and the cdproto definitions pinned in go.mod
	// (e.g. unknown PrivateNetworkRequestPolicy enum values, cookiePart parse
	// errors). They are harmless: the affected events are simply dropped.
	// TODO: implement proper logger handling.
	tabCtx, cancelTab := chromedp.NewContext(allocCtx,
		chromedp.WithLogf(func(string, ...any) {}),
		chromedp.WithErrorf(func(string, ...any) {}),
		chromedp.WithDebugf(func(string, ...any) {}),
	)
	defer cancelTab()

	store := newRequestStore()
	coll := newCollector()

	// screenshotCollector gathers screenshots taken concurrently at each
	// lifecycle stage.
	sc := &screenshotCollector{}

	chromedp.ListenTarget(tabCtx, func(ev any) {
		switch ev := ev.(type) {
		case *network.EventRequestWillBeSent:
			onRequest(ev, store, coll)
		case *network.EventResponseReceived:
			onResponse(ev, store, coll)
		case *page.EventLifecycleEvent:
			switch ev.Name {
			case string(StageDocumentLoad), string(StageFirstContentfulPaint):
				if opts.Screenshots {
					// Spawn immediately so the screenshot is taken at this
					// point in the page lifecycle, not deferred to later.
					sc.capture(tabCtx, LifecycleStage(ev.Name))
				}
			case string(StageNetworkIdle):
				if opts.Screenshots {
					sc.capture(tabCtx, StageNetworkIdle)
				}
				coll.markDone()
			}
		}
	})

	// Navigate with its own shorter deadline. A timeout here is not fatal —
	// events collected during a partial navigation are still valid HAR entries.
	// Any other error (DNS failure, invalid URL) is a hard stop.
	navCtx, cancelNav := context.WithTimeout(tabCtx, navTimeout)
	defer cancelNav()

	timedOut := false
	if err := chromedp.Run(navCtx,
		chromedp.EmulateViewport(viewportWidth, viewportHeight),
		chromedp.Navigate(opts.URL),
	); err != nil {
		if !isTimeoutError(err) {
			return nil, fmt.Errorf("capture: navigation failed: %w", err)
		}
		timedOut = true
	}

	pages, completedEntries, collTimedOut := coll.wait(totalCtx)
	timedOut = timedOut || collTimedOut

	// If we timed out before networkIdle, capture a final screenshot of
	// whatever state the page reached.
	if opts.Screenshots && timedOut {
		sc.capture(tabCtx, StageNetworkIdle)
	}

	// Wait for all in-flight screenshot goroutines to finish before assembling
	// the result.
	screenshots := sc.wait()

	h := assembleHAR(pages, completedEntries, browserVersion)
	return &Result{
		HAR:         h,
		TTFB:        extractTTFB(completedEntries),
		Screenshots: screenshots,
		TimedOut:    timedOut,
	}, nil
}

// screenshotCollector takes screenshots concurrently at each lifecycle stage
// and collects the results safely across goroutines.
type screenshotCollector struct {
	wg      sync.WaitGroup
	mu      sync.Mutex
	results []Screenshot
}

// capture spawns a goroutine that takes a screenshot immediately and appends
// the result to the collector. Safe to call from the CDP listener goroutine.
func (sc *screenshotCollector) capture(ctx context.Context, stage LifecycleStage) {
	sc.wg.Add(1)
	go func() {
		defer sc.wg.Done()
		var buf []byte
		if err := chromedp.Run(ctx, chromedp.CaptureScreenshot(&buf)); err != nil {
			return
		}
		sc.mu.Lock()
		sc.results = append(sc.results, Screenshot{
			Stage:      stage,
			CapturedAt: time.Now(),
			PNG:        buf,
		})
		sc.mu.Unlock()
	}()
}

// wait blocks until all in-flight screenshots have completed and returns them
// sorted into canonical lifecycle order.
func (sc *screenshotCollector) wait() []Screenshot {
	sc.wg.Wait()
	sort.Slice(sc.results, func(i, j int) bool {
		return lifecycleOrder[sc.results[i].Stage] < lifecycleOrder[sc.results[j].Stage]
	})
	return sc.results
}

// extractTTFB finds the document request among completed entries and returns
// the time between the request being sent and the first response byte received.
// Chrome exposes this as ReceiveHeadersStart in ResourceTiming, in milliseconds
// relative to requestTime.
func extractTTFB(entries []completedEntry) time.Duration {
	for _, e := range entries {
		if e.request.resourceType != network.ResourceTypeDocument {
			continue
		}
		t := e.response.Response.Timing
		if t == nil || t.ReceiveHeadersStart < 0 {
			return 0
		}
		return time.Duration(t.ReceiveHeadersStart * float64(time.Millisecond))
	}
	return 0
}

// onRequest processes an incoming request event. It registers the pending
// request in the store and, for document-type requests, emits a har.Page.
func onRequest(ev *network.EventRequestWillBeSent, store *requestStore, coll *collector) {
	pageRef := "page_" + string(ev.RequestID)

	store.addRequest(pendingRequest{
		requestID:    ev.RequestID,
		method:       ev.Request.Method,
		url:          ev.Request.URL,
		headers:      ev.Request.Headers,
		wallTime:     ev.WallTime.Time(),
		resourceType: ev.Type,
		pageRef:      pageRef,
	})

	if ev.Type == network.ResourceTypeDocument {
		coll.send(har.Page{
			ID:              pageRef,
			StartedDateTime: ev.WallTime.Time().Format(time.RFC3339Nano),
			Title:           ev.Request.URL,
			PageTimings:     &har.PageTimings{},
		})
	}
}

// onResponse attempts to correlate the response with its pending request and,
// on success, emits a completedEntry.
func onResponse(ev *network.EventResponseReceived, store *requestStore, coll *collector) {
	entry, ok := store.correlate(ev)
	if !ok {
		// The request was either never seen or already correlated — skip.
		return
	}
	coll.send(entry)
}

// isTimeoutError reports whether err stems from a context deadline or
// cancellation. Used to distinguish a navigation timeout (graceful) from a
// hard failure such as a DNS error.
func isTimeoutError(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// onceCloser ensures a channel is closed at most once, guarding against the
// case where networkIdle fires multiple times.
type onceCloser struct {
	ch   chan struct{}
	done bool
}

func (o *onceCloser) close() {
	if !o.done {
		o.done = true
		close(o.ch)
	}
}
