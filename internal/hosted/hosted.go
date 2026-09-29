// Package hosted is the HTTP middleware of the hosted profile (ADR-0005):
// client identification behind a trusted reverse proxy, per-client rate
// limiting, a global concurrency cap, and aggregate metrics keyed by MCP
// method and tool name. It never records query text (docs/security.md §2.7).
package hosted

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Options configures the middleware.
type Options struct {
	// Rate is the sustained requests per second allowed per client and
	// Burst the bucket size (defaults 1 rps, burst 30).
	Rate  float64
	Burst int
	// MaxInFlight caps concurrent requests across all clients (default 64).
	MaxInFlight int
	// TrustedProxies are the networks whose X-Forwarded-For is believed (the
	// reverse proxy in front of the server). Empty: the TCP peer is the client.
	TrustedProxies []netip.Prefix
	// MaxClients bounds the per-client state (default 100k); idle entries are
	// dropped first.
	MaxClients int
	Now        func() time.Time
}

// Middleware wraps an MCP handler.
type Middleware struct {
	opt      Options
	inflight chan struct{}
	mu       sync.Mutex
	clients  map[string]*bucket
	Metrics  *Metrics
}

type bucket struct {
	tokens float64
	last   time.Time
}

// New builds the middleware with defaults applied.
func New(opt Options) *Middleware {
	if opt.Rate <= 0 {
		opt.Rate = 1
	}
	if opt.Burst <= 0 {
		opt.Burst = 30
	}
	if opt.MaxInFlight <= 0 {
		opt.MaxInFlight = 64
	}
	if opt.MaxClients <= 0 {
		opt.MaxClients = 100_000
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Middleware{opt: opt, inflight: make(chan struct{}, opt.MaxInFlight), clients: map[string]*bucket{}, Metrics: NewMetrics()}
}

// ClientIP returns the client address: the TCP peer, or the right-most
// X-Forwarded-For entry that is not itself a trusted proxy when the peer is
// one (the only part of that header the proxy vouches for).
func (m *Middleware) ClientIP(r *http.Request) string {
	peer := peerAddr(r.RemoteAddr)
	if !peer.IsValid() || !m.trusted(peer) {
		return peer.String()
	}
	hops := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		a = a.Unmap()
		if !m.trusted(a) {
			return a.String()
		}
	}
	return peer.String()
}

func peerAddr(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	a, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return a.Unmap()
}

func (m *Middleware) trusted(a netip.Addr) bool {
	for _, p := range m.opt.TrustedProxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// allow takes one token from the client's bucket; when empty it returns the
// wait until the next token.
func (m *Middleware) allow(client string) (bool, time.Duration) {
	now := m.opt.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	b := m.clients[client]
	if b == nil {
		if len(m.clients) >= m.opt.MaxClients {
			m.evict(now)
		}
		b = &bucket{tokens: float64(m.opt.Burst), last: now}
		m.clients[client] = b
	}
	b.tokens = math.Min(float64(m.opt.Burst), b.tokens+now.Sub(b.last).Seconds()*m.opt.Rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / m.opt.Rate * float64(time.Second))
}

// evict drops clients whose bucket has refilled (idle), then, if still full,
// an arbitrary tenth: memory stays bounded under address spraying.
func (m *Middleware) evict(now time.Time) {
	full := time.Duration(float64(m.opt.Burst) / m.opt.Rate * float64(time.Second))
	for k, b := range m.clients {
		if now.Sub(b.last) > full {
			delete(m.clients, k)
		}
	}
	for k := range m.clients {
		if len(m.clients) < m.opt.MaxClients*9/10 {
			break
		}
		delete(m.clients, k)
	}
}

// Wrap applies rate limiting, the concurrency cap and metrics to next.
func (m *Middleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := m.opt.Now()
		method, tool := peek(r)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() { m.Metrics.observe(method, tool, rec.status, m.opt.Now().Sub(start)) }()
		if ok, wait := m.allow(m.ClientIP(r)); !ok {
			rec.Header().Set("Retry-After", strconv.Itoa(int(math.Ceil(wait.Seconds()))))
			http.Error(rec, "rate limited: slow down and retry after the Retry-After seconds", http.StatusTooManyRequests)
			return
		}
		select {
		case m.inflight <- struct{}{}:
			defer func() { <-m.inflight }()
		default:
			rec.Header().Set("Retry-After", "2")
			http.Error(rec, "server busy: retry shortly", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(rec, r)
	})
}

// peek reads the JSON-RPC method and tool name (tools/call params.name) of a
// POST without consuming the body. Nothing else of the request is kept.
func peek(r *http.Request) (method, tool string) {
	if r.Method != http.MethodPost || r.Body == nil {
		return r.Method, ""
	}
	b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(b), r.Body))
	if err != nil {
		return "invalid", ""
	}
	var msg struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if json.Unmarshal(b, &msg) != nil || msg.Method == "" {
		return "invalid", ""
	}
	if msg.Method == "tools/call" && validName(msg.Params.Name) {
		tool = msg.Params.Name
	}
	return validOr(msg.Method, "other"), tool
}

// validName keeps metric label cardinality bounded: only tool-like names.
func validName(s string) bool {
	if len(s) == 0 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		ok := c == '_' || c == '/' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
		if !ok {
			return false
		}
	}
	return true
}

func validOr(s, def string) string {
	if validName(s) {
		return s
	}
	return def
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
