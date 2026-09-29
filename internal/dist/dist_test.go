package dist

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
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

// oneChunkShard writes shard <name> holding the fixture's first chunk under
// another license.
func oneChunkShard(t *testing.T, dir, name, license string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(testfixture.Root(), "chunks.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var line string
	for _, l := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(l, "{") {
			line = l
			break
		}
	}
	var c map[string]any
	if err := json.Unmarshal([]byte(line), &c); err != nil {
		t.Fatal(err)
	}
	c["license"] = license
	lb, _ := json.Marshal(c)
	jl := filepath.Join(t.TempDir(), "u.jsonl")
	if err := os.WriteFile(jl, append(lb, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := index.BuildFromJSONL(context.Background(), filepath.Join(dir, name+".sqlite"), name, jl, ""); err != nil {
		t.Fatal(err)
	}
}

// Forum posts never ship, not even with --include-unlicensed.
func TestPublishNeverShipsUserContent(t *testing.T) {
	e := setup(t)
	oneChunkShard(t, e.in, "forum", "LicenseRef-ChiefDelphi-UserContent")
	for _, include := range []bool{false, true} {
		var skipped []string
		m, err := Publish(context.Background(), PublishOptions{InDir: e.in, OutDir: e.out, Channel: "stable", Key: e.priv,
			IncludeUnlicensed: include, OnSkip: func(s, _ string) { skipped = append(skipped, s) }})
		if err != nil {
			t.Fatal(err)
		}
		if len(m.Shards) != 1 || m.Shards[0].Name != "fixture" || len(skipped) != 1 || skipped[0] != "forum" {
			t.Fatalf("include=%v: shards %+v skipped %v", include, m.Shards, skipped)
		}
	}
}

func TestPublishSkipsUnlicensed(t *testing.T) {
	e := setup(t)
	// A second shard whose content has no redistribution license.
	oneChunkShard(t, e.in, "vendor-x", "LicenseRef-Vendor-NoLicense")
	var skipped []string
	m, err := Publish(context.Background(), PublishOptions{InDir: e.in, OutDir: e.out, Channel: "stable", Key: e.priv,
		OnSkip: func(s, _ string) { skipped = append(skipped, s) }})
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Shards) != 1 || m.Shards[0].Name != "fixture" || len(skipped) != 1 || skipped[0] != "vendor-x" {
		t.Fatalf("shards %+v skipped %v", m.Shards, skipped)
	}
	m, err = Publish(context.Background(), PublishOptions{InDir: e.in, OutDir: e.out, Channel: "stable", Key: e.priv, IncludeUnlicensed: true})
	if err != nil || len(m.Shards) != 2 {
		t.Fatalf("include: %v %+v", err, m)
	}
}

func TestProductionKeyCompiledIn(t *testing.T) {
	kr, err := TrustedKeyring("")
	if err != nil || len(kr) == 0 {
		t.Fatalf("no compiled-in key: %v", err)
	}
	found := false
	for _, k := range kr {
		found = found || KeyID(k) == "4d6c685f407388a5"
	}
	if !found {
		t.Fatalf("production key 4d6c685f407388a5 missing")
	}
}
