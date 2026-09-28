package dist

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/netguard"
	"github.com/fikretyukselit/frc-mcp/internal/testfixture"
)

type env struct {
	pub     ed25519.PublicKey
	priv    ed25519.PrivateKey
	in, out string
	srv     *httptest.Server
	client  *http.Client
}

func setup(t *testing.T) *env {
	t.Helper()
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	e := &env{pub: pub, priv: priv, in: t.TempDir(), out: t.TempDir()}
	root := testfixture.Root()
	if _, err := index.BuildFromJSONL(context.Background(), filepath.Join(e.in, "fixture.sqlite"), "fixture",
		filepath.Join(root, "chunks.jsonl"), filepath.Join(root, "symbols.jsonl")); err != nil {
		t.Fatal(err)
	}
	e.srv = httptest.NewTLSServer(http.FileServer(http.Dir(e.out)))
	t.Cleanup(e.srv.Close)
	pool := x509.NewCertPool()
	pool.AddCert(e.srv.Certificate())
	u, _ := url.Parse(e.srv.URL)
	e.client = (&netguard.Policy{Hosts: []string{u.Hostname()}, AllowNonPublic: true, TLSConfig: &tls.Config{RootCAs: pool}}).Client()
	return e
}

func (e *env) publish(t *testing.T, prev *Manifest) *Manifest {
	t.Helper()
	m, err := Publish(context.Background(), PublishOptions{InDir: e.in, OutDir: e.out, Channel: "stable", Prev: prev, Key: e.priv})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func (e *env) sync(t *testing.T, dir string, kr Keyring) (*SyncResult, error) {
	return Sync(context.Background(), SyncOptions{BaseURL: e.srv.URL, Dir: dir, Keyring: kr, Client: e.client})
}

func TestPublishSyncRoundTrip(t *testing.T) {
	e := setup(t)
	m := e.publish(t, nil)
	if m.Serial != 1 || len(m.Shards) != 1 || m.ExpiresAt.Before(time.Now()) {
		t.Fatalf("manifest %+v", m)
	}
	dst := t.TempDir()
	kr := Keyring{KeyID(e.pub): e.pub}
	res, err := e.sync(t, dst, kr)
	if err != nil || !res.Updated || res.Downloaded != 1 {
		t.Fatalf("sync: %+v %v", res, err)
	}
	if err := VerifyLocal(dst, kr); err != nil {
		t.Fatal(err)
	}
	shards, errs := index.OpenDir(context.Background(), dst)
	if len(shards) != 1 || len(errs) != 0 || shards[0].Meta().Chunks != 20 {
		t.Fatalf("open synced: %d shards %v", len(shards), errs)
	}
	shards[0].Close()
	// Re-sync of the same serial is a no-op.
	if res, err := e.sync(t, dst, kr); err != nil || res.Updated || res.Downloaded != 0 {
		t.Fatalf("idempotent: %+v %v", res, err)
	}
	// New serial, unchanged content: manifest advances, nothing re-downloaded.
	e.publish(t, m)
	if res, err := e.sync(t, dst, kr); err != nil || !res.Updated || res.Downloaded != 0 || res.Serial != 2 {
		t.Fatalf("delta: %+v %v", res, err)
	}
}

func TestRejectsUntrustedTamperedRollback(t *testing.T) {
	e := setup(t)
	m1 := e.publish(t, nil)
	kr := Keyring{KeyID(e.pub): e.pub}

	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err := e.sync(t, t.TempDir(), Keyring{KeyID(other): other}); !errors.Is(err, ErrUntrusted) {
		t.Fatalf("untrusted: %v", err)
	}
	if _, err := e.sync(t, t.TempDir(), Keyring{}); err == nil {
		t.Fatal("empty keyring must fail closed")
	}

	// Tampered manifest bytes.
	mp := filepath.Join(e.out, "manifest.json")
	orig, _ := os.ReadFile(mp)
	if err := os.WriteFile(mp, append(orig, ' '), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := e.sync(t, t.TempDir(), kr); !errors.Is(err, ErrBadSig) {
		t.Fatalf("tampered: %v", err)
	}
	if err := os.WriteFile(mp, orig, 0o644); err != nil {
		t.Fatal(err)
	}

	// Rollback: client has serial 2, server re-publishes serial 1.
	dst := t.TempDir()
	e.publish(t, m1) // serial 2
	if _, err := e.sync(t, dst, kr); err != nil {
		t.Fatal(err)
	}
	e.publish(t, nil) // serial 1 again (attacker replays an old, validly signed manifest)
	if _, err := e.sync(t, dst, kr); !errors.Is(err, ErrRollback) {
		t.Fatalf("rollback: %v", err)
	}
}

func TestCorruptObjectRejected(t *testing.T) {
	e := setup(t)
	m := e.publish(t, nil)
	obj := filepath.Join(e.out, m.Shards[0].DB.Object)
	b, _ := os.ReadFile(obj)
	b[len(b)/2] ^= 0xFF
	if err := os.WriteFile(obj, b, 0o644); err != nil {
		t.Fatal(err)
	}
	dst := t.TempDir()
	if _, err := e.sync(t, dst, Keyring{KeyID(e.pub): e.pub}); err == nil {
		t.Fatal("corrupt object must be rejected")
	}
	if _, err := os.Stat(filepath.Join(dst, "manifest.json")); !os.IsNotExist(err) {
		t.Fatal("a failed sync must not commit a manifest")
	}
}

func TestParseKeyring(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	kr, err := ParseKeyring(base64.StdEncoding.EncodeToString(pub))
	if err != nil || kr[KeyID(pub)] == nil {
		t.Fatalf("%v %v", kr, err)
	}
	if _, err := ParseKeyring("not-a-key"); err == nil {
		t.Fatal("want error")
	}
}
