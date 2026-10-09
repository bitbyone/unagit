package forge

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// A client's requests all go through one Transport: it follows the
// server's rate limit from the headers of every answer, so what polls in
// the background can stand back before the server says no, and, where the
// server tags its answers, it asks again with If-None-Match - an unchanged
// answer is 304, which GitHub does not count against the limit, and the
// body kept from before is handed back as if it had come again.

// RateHeaders are the names a forge gives its rate limit headers.
type RateHeaders struct {
	Limit, Remaining, Reset string
}

var (
	// GitLabRates are GitLab's: RateLimit-Limit, -Remaining, -Reset.
	GitLabRates = RateHeaders{"RateLimit-Limit", "RateLimit-Remaining", "RateLimit-Reset"}
	// GitHubRates are GitHub's: X-RateLimit-Limit, -Remaining, -Reset.
	GitHubRates = RateHeaders{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"}
)

// rateReserve is how much of its limit a server keeps for what the user
// asks for: below it, the background stands back until the limit resets.
const rateReserve = 50

// Transport wraps an http.RoundTripper with the rate limit's following and,
// when ETags is set, the conditional GETs.
type Transport struct {
	Base    http.RoundTripper
	Headers RateHeaders
	// ETags keeps GET answers that came with an ETag, to ask again with
	// If-None-Match.
	ETags bool

	mu    sync.Mutex
	until time.Time
	kept  map[string]keptAnswer
	order []string
}

type keptAnswer struct {
	etag   string
	header http.Header
	body   []byte
}

// keptAnswers is how many answers Transport keeps for If-None-Match.
const keptAnswers = 500

// PausedUntil is when the server may be asked again by what polls in the
// background; zero when it may be now.
func (t *Transport) PausedUntil() time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	if time.Now().After(t.until) {
		return time.Time{}
	}
	return t.until
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	key := ""
	var kept keptAnswer
	if t.ETags && req.Method == http.MethodGet {
		key = req.URL.String()
		t.mu.Lock()
		kept = t.kept[key]
		t.mu.Unlock()
		if kept.etag != "" {
			req = req.Clone(req.Context())
			req.Header.Set("If-None-Match", kept.etag)
		}
	}
	resp, err := base.RoundTrip(req)
	if err != nil {
		return nil, err
	}
	t.note(resp.Header, resp.StatusCode, time.Now())
	switch {
	case key != "" && resp.StatusCode == http.StatusNotModified && kept.etag != "":
		resp.Body.Close()
		header := kept.header.Clone()
		// The rate limit's headers are this answer's.
		for _, h := range []string{t.Headers.Limit, t.Headers.Remaining, t.Headers.Reset} {
			if v := resp.Header.Get(h); v != "" {
				header.Set(h, v)
			}
		}
		resp.StatusCode, resp.Status, resp.Header = http.StatusOK, "200 OK", header
		resp.Body = io.NopCloser(bytes.NewReader(kept.body))
		resp.ContentLength = int64(len(kept.body))
	case key != "" && resp.StatusCode == http.StatusOK && resp.Header.Get("ETag") != "":
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		resp.Body = io.NopCloser(bytes.NewReader(body))
		t.keep(key, keptAnswer{etag: resp.Header.Get("ETag"), header: resp.Header.Clone(), body: body})
	}
	return resp, nil
}

// keep remembers an answer, the oldest going once there are too many.
func (t *Transport) keep(key string, a keptAnswer) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.kept == nil {
		t.kept = map[string]keptAnswer{}
	}
	if _, ok := t.kept[key]; !ok {
		t.order = append(t.order, key)
	}
	t.kept[key] = a
	for len(t.order) > keptAnswers {
		delete(t.kept, t.order[0])
		t.order = t.order[1:]
	}
}

// note reads the rate limit off an answer: refused for it - 429, or 403
// with nothing remaining - stand back until Retry-After or the reset; with
// little left, until the reset.
func (t *Transport) note(h http.Header, status int, now time.Time) {
	remaining, haveRemaining := headerInt(h, t.Headers.Remaining)
	limit, _ := headerInt(h, t.Headers.Limit)
	reset := time.Time{}
	if r, ok := headerInt(h, t.Headers.Reset); ok {
		reset = time.Unix(int64(r), 0)
	}
	var until time.Time
	switch {
	case status == http.StatusTooManyRequests || status == http.StatusForbidden && haveRemaining && remaining == 0:
		until = now.Add(time.Minute)
		if s, ok := headerInt(h, "Retry-After"); ok {
			until = now.Add(time.Duration(s) * time.Second)
		} else if reset.After(now) {
			until = reset
		}
	case haveRemaining && limit > 0 && remaining <= min(rateReserve, limit/10) && reset.After(now):
		until = reset
	default:
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if until.After(t.until) {
		t.until = until
	}
}

func headerInt(h http.Header, name string) (int, bool) {
	if name == "" {
		return 0, false
	}
	v := h.Get(name)
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	return n, err == nil
}
