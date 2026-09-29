package hosted

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Metrics are aggregate counters in Prometheus text format: requests by
// (method, tool, status class) and a latency histogram by (method, tool).
// Labels come from a bounded vocabulary; no request content is stored.
type Metrics struct {
	mu      sync.Mutex
	count   map[[3]string]uint64
	buckets map[[2]string][]uint64 // cumulative-on-render counts per bound
	sum     map[[2]string]float64
	n       map[[2]string]uint64
	started time.Time
}

var bounds = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

// maxSeries bounds the number of label combinations kept.
const maxSeries = 512

// NewMetrics returns empty counters.
func NewMetrics() *Metrics {
	return &Metrics{count: map[[3]string]uint64{}, buckets: map[[2]string][]uint64{}, sum: map[[2]string]float64{},
		n: map[[2]string]uint64{}, started: time.Now()}
}

func (m *Metrics) observe(method, tool string, status int, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [3]string{method, tool, fmt.Sprintf("%dxx", status/100)}
	if _, ok := m.count[k]; !ok && len(m.count) >= maxSeries {
		k = [3]string{"other", "", k[2]}
	}
	m.count[k]++
	hk := [2]string{k[0], k[1]}
	if m.buckets[hk] == nil {
		m.buckets[hk] = make([]uint64, len(bounds))
	}
	s := d.Seconds()
	for i, b := range bounds {
		if s <= b {
			m.buckets[hk][i]++
			break
		}
	}
	m.sum[hk] += s
	m.n[hk]++
}

// ServeHTTP renders the counters.
func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var b strings.Builder
	b.WriteString("# HELP frc_mcp_requests_total MCP HTTP requests by JSON-RPC method, tool and status class.\n# TYPE frc_mcp_requests_total counter\n")
	keys := make([][3]string, 0, len(m.count))
	for k := range m.count {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return strings.Join(keys[i][:], "|") < strings.Join(keys[j][:], "|") })
	for _, k := range keys {
		fmt.Fprintf(&b, "frc_mcp_requests_total{method=%q,tool=%q,status=%q} %d\n", k[0], k[1], k[2], m.count[k])
	}
	b.WriteString("# HELP frc_mcp_request_seconds MCP HTTP request latency.\n# TYPE frc_mcp_request_seconds histogram\n")
	hk := make([][2]string, 0, len(m.n))
	for k := range m.n {
		hk = append(hk, k)
	}
	sort.Slice(hk, func(i, j int) bool { return hk[i][0]+hk[i][1] < hk[j][0]+hk[j][1] })
	for _, k := range hk {
		var cum uint64
		for i, bound := range bounds {
			cum += m.buckets[k][i]
			fmt.Fprintf(&b, "frc_mcp_request_seconds_bucket{method=%q,tool=%q,le=\"%g\"} %d\n", k[0], k[1], bound, cum)
		}
		fmt.Fprintf(&b, "frc_mcp_request_seconds_bucket{method=%q,tool=%q,le=\"+Inf\"} %d\n", k[0], k[1], m.n[k])
		fmt.Fprintf(&b, "frc_mcp_request_seconds_sum{method=%q,tool=%q} %g\n", k[0], k[1], m.sum[k])
		fmt.Fprintf(&b, "frc_mcp_request_seconds_count{method=%q,tool=%q} %d\n", k[0], k[1], m.n[k])
	}
	fmt.Fprintf(&b, "# TYPE frc_mcp_uptime_seconds gauge\nfrc_mcp_uptime_seconds %g\n", time.Since(m.started).Seconds())
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	_, _ = w.Write([]byte(b.String()))
}
