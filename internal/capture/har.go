package capture

import (
	"fmt"
	"time"

	"github.com/chromedp/cdproto/har"
	"github.com/chromedp/cdproto/network"
)

// assembleHAR constructs a har.HAR from a slice of completed entries and a
// page map (keyed by page ref string).
func assembleHAR(pages []har.Page, entries []completedEntry, browserVersion string) har.HAR {
	h := har.HAR{
		Log: &har.Log{
			Version: "1.2",
			Browser: &har.Creator{
				Name:    "Google Chrome",
				Version: browserVersion,
			},
			Creator: &har.Creator{
				Name:    "har-capture",
				Version: "0.1.0",
			},
			Pages:   make([]*har.Page, 0, len(pages)),
			Entries: make([]*har.Entry, 0, len(entries)),
		},
	}

	for i := range pages {
		p := pages[i]
		h.Log.Pages = append(h.Log.Pages, &p)
	}

	for _, e := range entries {
		entry := buildEntry(e)
		h.Log.Entries = append(h.Log.Entries, &entry)
	}

	return h
}

func buildEntry(e completedEntry) har.Entry {
	req := e.request
	resp := e.response

	entry := har.Entry{
		Pageref:         req.pageRef,
		StartedDateTime: req.wallTime.Format(time.RFC3339Nano),
		Request: &har.Request{
			Method:      req.method,
			URL:         req.url,
			HTTPVersion: resp.Response.Protocol,
			Headers:     headersToHAR(req.headers),
			QueryString: []*har.NameValuePair{},
			Cookies:     []*har.Cookie{},
			HeadersSize: -1,
			BodySize:    -1,
		},
		Response: &har.Response{
			Status:      int64(resp.Response.Status),
			StatusText:  resp.Response.StatusText,
			HTTPVersion: resp.Response.Protocol,
			Headers:     headersToHAR(resp.Response.Headers),
			Cookies:     []*har.Cookie{},
			Content: &har.Content{
				MimeType: resp.Response.MimeType,
				Size:     0, // Populated separately if body capture is enabled.
			},
			RedirectURL: redirectURL(resp.Response.Headers),
			HeadersSize: -1,
			BodySize:    -1,
		},
		Timings: buildTimings(resp.Response.Timing),
	}

	// Total time is the sum of all non-negative timings.
	entry.Time = totalTime(entry.Timings)

	return entry
}

func buildTimings(t *network.ResourceTiming) *har.Timings {
	if t == nil {
		return &har.Timings{Send: -1, Wait: -1, Receive: -1}
	}

	// Chrome's ResourceTiming values are in milliseconds relative to
	// requestTime. A value of -1 means the phase did not occur.
	dns := phaseOrBlocked(t.DNSStart, t.DNSEnd)
	connect := phaseOrBlocked(t.ConnectStart, t.ConnectEnd)
	ssl := phaseOrBlocked(t.SslStart, t.SslEnd)
	send := phaseOrBlocked(t.SendStart, t.SendEnd)

	// Wait = from send end to first byte received (receiveHeadersEnd).
	wait := float64(-1)
	if t.SendEnd >= 0 && t.ReceiveHeadersEnd >= 0 {
		wait = t.ReceiveHeadersEnd - t.SendEnd
	}

	return &har.Timings{
		Blocked: -1,
		DNS:     dns,
		Connect: connect,
		Ssl:     ssl,
		Send:    send,
		Wait:    wait,
		Receive: -1, // Requires body download tracking; not available here.
	}
}

func phaseOrBlocked(start, end float64) float64 {
	if start < 0 || end < 0 {
		return -1
	}
	return end - start
}

func totalTime(t *har.Timings) float64 {
	total := float64(0)
	for _, v := range []float64{t.Blocked, t.DNS, t.Connect, t.Send, t.Wait, t.Receive} {
		if v > 0 {
			total += v
		}
	}
	return total
}

func redirectURL(headers network.Headers) string {
	for k, v := range map[string]any(headers) {
		if k == "Location" || k == "location" {
			return fmt.Sprint(v)
		}
	}
	return ""
}

func headersToHAR(headers network.Headers) []*har.NameValuePair {
	pairs := make([]*har.NameValuePair, 0, len(headers))
	for name, values := range map[string]any(headers) {
		if arr, ok := values.([]string); ok {
			for _, value := range arr {
				pairs = append(pairs, &har.NameValuePair{Name: name, Value: value})
			}
		} else {
			pairs = append(pairs, &har.NameValuePair{Name: name, Value: fmt.Sprint(values)})
		}
	}
	return pairs
}
