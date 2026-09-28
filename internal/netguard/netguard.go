// Package netguard is the only way frc-mcp talks to the network
// (docs/security.md §2.3). It enforces, below the application:
//
//   - https only;
//   - a host allowlist (derived from data/sources.yaml), re-checked on every
//     redirect;
//   - no connections to loopback, private, link-local, CGNAT, multicast or
//     other special-purpose addresses, checked on the resolved IP at dial time
//     (defeats DNS rebinding and redirects to internal hosts);
//   - no environment proxies (a proxy would bypass the IP check);
//   - response body caps and per-request deadlines.
package netguard

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// Errors returned (wrapped) by the guard.
var (
	ErrScheme    = errors.New("netguard: only https is allowed")
	ErrHost      = errors.New("netguard: host not on the allowlist")
	ErrAddress   = errors.New("netguard: destination address is not public")
	ErrTooLarge  = errors.New("netguard: response body exceeds limit")
	ErrRedirects = errors.New("netguard: too many redirects")
)

// Policy configures a guarded client.
type Policy struct {
	// Hosts are exact host names ("docs.wpilib.org") or suffix wildcards
	// ("*.revrobotics.com", matching subdomains only).
	Hosts []string
	// MaxBody caps response bodies read through Client (default 8 MiB).
	MaxBody int64
	// Timeout is the per-request deadline (default 20 s).
	Timeout time.Duration
	// UserAgent is sent on every request.
	UserAgent string

	// Test-only knobs.
	AllowNonPublic bool        // permit loopback/private destinations
	TLSConfig      *tls.Config // custom roots
}

// Allowed reports whether host passes the allowlist.
func (p *Policy) Allowed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, h := range p.Hosts {
		h = strings.ToLower(h)
		if suffix, ok := strings.CutPrefix(h, "*."); ok {
			if strings.HasSuffix(host, "."+suffix) {
				return true
			}
		} else if host == h {
			return true
		}
	}
	return false
}

// CheckURL validates scheme and host of a URL string.
func (p *Policy) CheckURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	return p.checkURL(u)
}

func (p *Policy) check(r *http.Request) error { return p.checkURL(r.URL) }

func (p *Policy) checkURL(u *url.URL) error {
	if u.Scheme != "https" {
		return fmt.Errorf("%w: %s", ErrScheme, u.Redacted())
	}
	if u.User != nil {
		return fmt.Errorf("%w: credentials in URL", ErrHost)
	}
	if !p.Allowed(u.Hostname()) {
		return fmt.Errorf("%w: %s", ErrHost, u.Hostname())
	}
	return nil
}

// Client returns an *http.Client enforcing the policy. Bodies returned by it
// are capped at MaxBody (reads beyond return ErrTooLarge).
func (p *Policy) Client() *http.Client {
	timeout := p.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
	if !p.AllowNonPublic {
		dialer.Control = controlPublicOnly
	}
	tlsCfg := p.TLSConfig
	if tlsCfg == nil {
		tlsCfg = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	tr := &http.Transport{
		Proxy:                 nil, // never honor env proxies: they would bypass the dial-time IP check
		DialContext:           dialer.DialContext,
		TLSClientConfig:       tlsCfg,
		ForceAttemptHTTP2:     true,
		MaxIdleConnsPerHost:   4,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 15 * time.Second,
	}
	maxBody := p.MaxBody
	if maxBody <= 0 {
		maxBody = 8 << 20
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &guarded{p: p, next: tr, maxBody: maxBody},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return ErrRedirects
			}
			return p.check(req)
		},
	}
}

type guarded struct {
	p       *Policy
	next    http.RoundTripper
	maxBody int64
}

func (g *guarded) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := g.p.check(r); err != nil {
		return nil, err
	}
	if g.p.UserAgent != "" && r.Header.Get("User-Agent") == "" {
		r = r.Clone(r.Context())
		r.Header.Set("User-Agent", g.p.UserAgent)
	}
	resp, err := g.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if resp.ContentLength > g.maxBody {
		resp.Body.Close()
		return nil, fmt.Errorf("%w: Content-Length %d > %d", ErrTooLarge, resp.ContentLength, g.maxBody)
	}
	resp.Body = &capped{rc: resp.Body, left: g.maxBody}
	return resp, nil
}

type capped struct {
	rc   io.ReadCloser
	left int64
}

func (c *capped) Read(p []byte) (int, error) {
	if c.left <= 0 {
		// Probe one byte to distinguish "exactly at limit" from "over".
		var b [1]byte
		if n, _ := c.rc.Read(b[:]); n > 0 {
			return 0, ErrTooLarge
		}
		return 0, io.EOF
	}
	if int64(len(p)) > c.left {
		p = p[:c.left]
	}
	n, err := c.rc.Read(p)
	c.left -= int64(n)
	return n, err
}

func (c *capped) Close() error { return c.rc.Close() }

// nonPublic are special-purpose ranges that must never be dialed
// (RFC 6890 and friends), beyond what netip's predicates cover.
var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), // CGNAT
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"), // NAT64 can embed private IPv4
	netip.MustParsePrefix("2001:db8::/32"),
}

// Public reports whether addr is a globally routable unicast address.
func Public(addr netip.Addr) bool {
	addr = addr.Unmap()
	if !addr.IsValid() || addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsInterfaceLocalMulticast() || addr.IsMulticast() ||
		addr.IsUnspecified() || !addr.IsGlobalUnicast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(addr) {
			return false
		}
	}
	return true
}

// controlPublicOnly runs after DNS resolution, on the exact IP being dialed.
func controlPublicOnly(_, address string, _ syscall.RawConn) error {
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		return fmt.Errorf("%w: %s", ErrAddress, address)
	}
	if !Public(ap.Addr()) {
		return fmt.Errorf("%w: %s", ErrAddress, ap.Addr())
	}
	return nil
}

// Get is a convenience GET with context through a guarded client.
func Get(ctx context.Context, c *http.Client, url string, hdr http.Header) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	for k, v := range hdr {
		req.Header[k] = v
	}
	return c.Do(req)
}
