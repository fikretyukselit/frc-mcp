package netguard

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
)

func TestAllowed(t *testing.T) {
	p := &Policy{Hosts: []string{"docs.wpilib.org", "*.revrobotics.com"}}
	for host, want := range map[string]bool{
		"docs.wpilib.org": true, "DOCS.WPILIB.ORG.": true, "evil-docs.wpilib.org": false,
		"docs.revrobotics.com": true, "revrobotics.com": false, "revrobotics.com.evil.io": false,
		"x.docs.wpilib.org": false,
	} {
		if got := p.Allowed(host); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", host, got, want)
		}
	}
}

func TestCheckURL(t *testing.T) {
	p := &Policy{Hosts: []string{"docs.wpilib.org"}}
	for raw, want := range map[string]error{
		"https://docs.wpilib.org/en/stable/": nil,
		"http://docs.wpilib.org/":            ErrScheme,
		"https://evil.example/":              ErrHost,
		"https://user:pw@docs.wpilib.org/":   ErrHost,
		"file:///etc/passwd":                 ErrScheme,
	} {
		if err := p.CheckURL(raw); !errors.Is(err, want) && (want != nil || err != nil) {
			t.Errorf("CheckURL(%q) = %v, want %v", raw, err, want)
		}
	}
}

func TestPublic(t *testing.T) {
	for s, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "::1": false, "fe80::1": false, "fc00::1": false,
		"::ffff:127.0.0.1": false, "64:ff9b::a00:1": false, "224.0.0.1": false, "240.0.0.1": false,
	} {
		if got := Public(netip.MustParseAddr(s)); got != want {
			t.Errorf("Public(%s) = %v, want %v", s, got, want)
		}
	}
}

// tlsServer returns an HTTPS test server and a policy that trusts it.
func tlsServer(t *testing.T, h http.Handler) (*httptest.Server, *Policy) {
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(srv.Certificate())
	u, _ := url.Parse(srv.URL)
	return srv, &Policy{Hosts: []string{u.Hostname()}, TLSConfig: &tls.Config{RootCAs: pool}}
}

func TestBlocksLoopbackAtDialTime(t *testing.T) {
	srv, p := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { io.WriteString(w, "secret") }))
	// Allowlisted host, but it resolves to 127.0.0.1: must be refused.
	resp, err := Get(context.Background(), p.Client(), srv.URL, nil)
	if err == nil {
		resp.Body.Close()
	}
	if !errors.Is(err, ErrAddress) {
		t.Fatalf("want ErrAddress, got %v", err)
	}
	p.AllowNonPublic = true
	resp, err = Get(context.Background(), p.Client(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestRedirectToDisallowedHost(t *testing.T) {
	srv, p := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://evil.example/steal", http.StatusFound)
	}))
	p.AllowNonPublic = true
	resp, err := Get(context.Background(), p.Client(), srv.URL, nil)
	if err == nil {
		resp.Body.Close()
	}
	if !errors.Is(err, ErrHost) {
		t.Fatalf("want ErrHost on redirect, got %v", err)
	}
}

func TestBodyCap(t *testing.T) {
	big := strings.Repeat("x", 4096)
	srv, p := tlsServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.(http.Flusher).Flush() // chunked: no Content-Length, cap enforced while reading
		io.WriteString(w, big)
	}))
	p.AllowNonPublic, p.MaxBody = true, 1024
	resp, err := Get(context.Background(), p.Client(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if _, err := io.ReadAll(resp.Body); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("want ErrTooLarge, got %v", err)
	}
}

func TestUserAgent(t *testing.T) {
	var ua string
	srv, p := tlsServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { ua = r.UserAgent() }))
	p.AllowNonPublic, p.UserAgent = true, "frc-mcp-indexer/test (+https://github.com/fikretyukselit/frc-mcp)"
	resp, err := Get(context.Background(), p.Client(), srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if ua != p.UserAgent {
		t.Fatalf("UA = %q", ua)
	}
}
