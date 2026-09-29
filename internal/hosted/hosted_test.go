package hosted

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"
)

func req(remote, xff, body string) *http.Request {
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.RemoteAddr = remote
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return r
}

func TestClientIP(t *testing.T) {
	m := New(Options{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("172.18.0.0/16")}})
	for _, tc := range []struct{ remote, xff, want string }{
		{"203.0.113.9:5555", "", "203.0.113.9"},
		{"203.0.113.9:5555", "1.2.3.4", "203.0.113.9"},                  // untrusted peer: header ignored (spoofing)
		{"172.18.0.2:4444", "1.2.3.4", "1.2.3.4"},                       // trusted proxy
		{"172.18.0.2:4444", "6.6.6.6, 198.51.100.7", "198.51.100.7"},    // right-most hop the proxy saw
		{"172.18.0.2:4444", "198.51.100.7, 172.18.0.3", "198.51.100.7"}, // chained trusted proxies skipped
		{"172.18.0.2:4444", "garbage", "172.18.0.2"},                    // unparsable: fall back to the peer
		{"[::ffff:203.0.113.9]:1", "", "203.0.113.9"},                   // v4-mapped
	} {
		if got := m.ClientIP(req(tc.remote, tc.xff, "")); got != tc.want {
			t.Errorf("ClientIP(%s, %q) = %s, want %s", tc.remote, tc.xff, got, tc.want)
		}
	}
}

func TestRateLimitAndRefill(t *testing.T) {
	now := time.Unix(0, 0)
	m := New(Options{Rate: 2, Burst: 3, Now: func() time.Time { return now }})
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	codes := func(n int, remote string) []int {
		var out []int
		for range n {
			w := httptest.NewRecorder()
			h.ServeHTTP(w, req(remote, "", `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
			out = append(out, w.Code)
		}
		return out
	}
	got := codes(4, "203.0.113.1:1")
	if got[0] != 200 || got[2] != 200 || got[3] != http.StatusTooManyRequests {
		t.Fatalf("burst: %v", got)
	}
	// Another client is unaffected.
	if c := codes(1, "203.0.113.2:1"); c[0] != 200 {
		t.Fatalf("other client: %v", c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("203.0.113.1:1", "", "{}"))
	if w.Header().Get("Retry-After") != "1" {
		t.Errorf("Retry-After = %q", w.Header().Get("Retry-After"))
	}
	now = now.Add(time.Second) // 2 tokens back
	if c := codes(3, "203.0.113.1:1"); c[0] != 200 || c[1] != 200 || c[2] != 429 {
		t.Fatalf("refill: %v", c)
	}
}

func TestConcurrencyCap(t *testing.T) {
	m := New(Options{MaxInFlight: 1, Rate: 1000, Burst: 1000})
	release := make(chan struct{})
	started := make(chan struct{})
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
	}))
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); h.ServeHTTP(httptest.NewRecorder(), req("203.0.113.1:1", "", "{}")) }()
	<-started
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req("203.0.113.2:1", "", "{}"))
	close(release)
	wg.Wait()
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("second request while full: %d", w.Code)
	}
}

func TestMetricsKeepNoContentAndBodyIntact(t *testing.T) {
	m := New(Options{})
	var seen []string
	h := m.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		seen = append(seen, string(b))
	}))
	body := `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"frc_search","arguments":{"query":"secret team strategy"}}}`
	h.ServeHTTP(httptest.NewRecorder(), req("203.0.113.1:1", "", body))
	h.ServeHTTP(httptest.NewRecorder(), req("203.0.113.1:1", "", `{"method":"tools/call","params":{"name":"x\"} DROP"}}`))
	if len(seen) != 2 || seen[0] != body {
		t.Fatal("handler did not get the full body")
	}
	w := httptest.NewRecorder()
	m.Metrics.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	out := w.Body.String()
	if !strings.Contains(out, `frc_mcp_requests_total{method="tools/call",tool="frc_search",status="2xx"} 1`) {
		t.Errorf("missing tool counter:\n%s", out)
	}
	if strings.Contains(out, "secret") || strings.Contains(out, "DROP") {
		t.Errorf("metrics leak request content:\n%s", out)
	}
}

func TestEvictionBoundsMemory(t *testing.T) {
	now := time.Unix(0, 0)
	m := New(Options{MaxClients: 100, Now: func() time.Time { return now }})
	for i := range 1000 {
		m.allow(netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)}).String())
	}
	if n := len(m.clients); n > 100 {
		t.Fatalf("clients = %d, want ≤ 100", n)
	}
}
