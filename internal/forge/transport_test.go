package forge

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// TestAServerShortOfItsLimitIsLeftAloneUntilItResets: plenty left is no
// pause; little left pauses until the reset; a refusal pauses for
// Retry-After.
func TestAServerShortOfItsLimitIsLeftAloneUntilItResets(t *testing.T) {
	var remaining, retry atomic.Int64
	remaining.Store(4000)
	reset := time.Now().Add(10 * time.Minute).Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(remaining.Load(), 10))
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset.Unix(), 10))
		if s := retry.Load(); s > 0 {
			w.Header().Set("Retry-After", strconv.FormatInt(s, 10))
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		fmt.Fprint(w, "{}")
	}))
	defer srv.Close()
	tr := &Transport{Headers: GitHubRates}
	c := &http.Client{Transport: tr}
	get := func() {
		resp, err := c.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	get()
	if !tr.PausedUntil().IsZero() {
		t.Fatal("paused with plenty left")
	}
	remaining.Store(20)
	get()
	if got := tr.PausedUntil(); !got.Equal(reset) {
		t.Fatalf("paused until %v, want the reset %v", got, reset)
	}

	tr2 := &Transport{Headers: GitHubRates}
	c = &http.Client{Transport: tr2}
	remaining.Store(4000)
	retry.Store(30)
	get()
	if got := time.Until(tr2.PausedUntil()); got < 25*time.Second || got > 31*time.Second {
		t.Fatalf("refused, paused for %v, want Retry-After's 30s", got)
	}
}

// TestAnUnchangedAnswerComesFromWhatWasKept: asked again with the ETag, a
// 304 hands back the body kept from before.
func TestAnUnchangedAnswerComesFromWhatWasKept(t *testing.T) {
	var asked, conditional atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if r.Header.Get("If-None-Match") == `"v1"` {
			conditional.Add(1)
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Link", `<next>; rel="next"`)
		fmt.Fprint(w, `{"n":1}`)
	}))
	defer srv.Close()
	c := &http.Client{Transport: &Transport{Headers: GitHubRates, ETags: true}}
	for range 3 {
		resp, err := c.Get(srv.URL + "/x")
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || string(body) != `{"n":1}` || resp.Header.Get("Link") == "" {
			t.Fatalf("got %d %q %v", resp.StatusCode, body, resp.Header)
		}
	}
	if asked.Load() != 3 || conditional.Load() != 2 {
		t.Fatalf("asked %d times, %d of them with the ETag", asked.Load(), conditional.Load())
	}
}
