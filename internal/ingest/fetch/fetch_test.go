package fetch

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// roundTrip counts requests and answers each with a fixed status.
type roundTrip struct {
	n      int
	status int
}

func (rt *roundTrip) RoundTrip(r *http.Request) (*http.Response, error) {
	rt.n++
	return &http.Response{StatusCode: rt.status, Body: io.NopCloser(strings.NewReader("<rss/>")), Header: http.Header{},
		Request: r}, nil
}

func TestGetFreshHonorsMinInterval(t *testing.T) {
	rt := &roundTrip{status: http.StatusOK}
	f := &Fetcher{Client: &http.Client{Transport: rt}, Dir: t.TempDir()}
	ctx := context.Background()
	const u = "https://forum.example/latest.rss"

	first, err := f.GetFresh(ctx, u, 30*time.Minute)
	if err != nil || rt.n != 1 || first.NotModified {
		t.Fatalf("first fetch: n=%d err=%v %+v", rt.n, err, first)
	}
	// Inside the floor: served from cache, no request.
	again, err := f.Within(30*time.Minute).Get(ctx, u)
	if err != nil || rt.n != 1 || !again.NotModified || again.SHA256 != first.SHA256 {
		t.Fatalf("within floor: n=%d err=%v %+v", rt.n, err, again)
	}
	// No floor (plain Get): the host is asked again.
	if _, err := f.Get(ctx, u); err != nil || rt.n != 2 {
		t.Fatalf("plain get: n=%d err=%v", rt.n, err)
	}
	// A floor shorter than the cache age: asked again.
	if _, err := f.GetFresh(ctx, u, time.Nanosecond); err != nil || rt.n != 3 {
		t.Fatalf("expired floor: n=%d err=%v", rt.n, err)
	}
}
