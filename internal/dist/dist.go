// Package dist distributes index shards (ADR-0003, ADR-0006):
//
//	publish: shards + vector layers + model files → gzip, content-addressed
//	         names → manifest.json (monotonic serial, expiry, digests)
//	         → manifest.json.sig (ed25519)
//	sync:    fetch manifest + signature → verify against the compiled-in /
//	         configured keyring → reject rollback (lower serial) → download
//	         only files whose digest changed → verify sha256 → atomic commit of
//	         the local manifest → prune unreferenced files
//
// Everything is plain HTTPS through netguard, so the same code serves GitHub
// Releases, a school mirror, or a USB stick copied to a local web server.
package dist

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ManifestSchema is the manifest format version this binary understands.
const ManifestSchema = 1

// Manifest describes one published index release.
type Manifest struct {
	Schema      int       `json:"schema"`
	Serial      int64     `json:"serial"`  // strictly increasing per channel; lower = rollback
	Channel     string    `json:"channel"` // stable | beta | alpha
	CreatedAt   time.Time `json:"created_at"`
	ExpiresAt   time.Time `json:"expires_at"` // past expiry: keep serving, report index_stale
	IndexSchema int       `json:"index_schema"`
	Shards      []Shard   `json:"shards"`
	Models      []File    `json:"models,omitempty"`
}

// Shard is one shard plus its optional vector layers.
type Shard struct {
	Name    string   `json:"name"`
	BuildID string   `json:"build_id"`
	Seasons []string `json:"seasons,omitempty"`
	DB      File     `json:"db"`
	Vectors []File   `json:"vectors,omitempty"`
}

// File is a published, gzip-compressed file.
type File struct {
	Name     string `json:"name"`      // local file name after decompression (relative)
	Object   string `json:"object"`    // published object name (content-addressed, .gz)
	SHA256   string `json:"sha256"`    // of the decompressed bytes
	GzSHA256 string `json:"gz_sha256"` // of the published object
	Size     int64  `json:"size"`
	GzSize   int64  `json:"gz_size"`
}

// Signature is the detached signature document (manifest.json.sig).
type Signature struct {
	KeyID string `json:"key_id"`
	Sig   string `json:"sig"` // base64 ed25519 signature over the exact manifest bytes
}

// Keyring holds trusted ed25519 public keys by key id.
type Keyring map[string]ed25519.PublicKey

// KeyID is the first 8 bytes (hex) of sha256(public key).
func KeyID(pub ed25519.PublicKey) string {
	h := sha256.Sum256(pub)
	return hex.EncodeToString(h[:8])
}

// ParseKeyring parses base64 public keys (comma/space separated).
func ParseKeyring(s string) (Keyring, error) {
	kr := Keyring{}
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' }) {
		b, err := base64.StdEncoding.DecodeString(f)
		if err != nil || len(b) != ed25519.PublicKeySize {
			return nil, fmt.Errorf("dist: invalid ed25519 public key %q", f)
		}
		kr[KeyID(b)] = ed25519.PublicKey(b)
	}
	return kr, nil
}

// Errors.
var (
	ErrUntrusted = errors.New("dist: manifest signature is not from a trusted key")
	ErrBadSig    = errors.New("dist: manifest signature is invalid")
	ErrRollback  = errors.New("dist: manifest serial is lower than the installed one (rollback refused)")
	ErrSchema    = errors.New("dist: manifest or index schema not supported by this binary; update frc-mcp")
	ErrDigest    = errors.New("dist: downloaded file does not match the manifest digest")
)

// Verify checks a detached signature against the keyring.
func (kr Keyring) Verify(manifest, sigDoc []byte) error {
	var sig Signature
	if err := json.Unmarshal(sigDoc, &sig); err != nil {
		return fmt.Errorf("%w: %w", ErrBadSig, err)
	}
	pub, ok := kr[sig.KeyID]
	if !ok {
		return fmt.Errorf("%w (key %s)", ErrUntrusted, sig.KeyID)
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Sig)
	if err != nil || !ed25519.Verify(pub, manifest, raw) {
		return ErrBadSig
	}
	return nil
}

// Sign produces a signature document for manifest bytes.
func Sign(priv ed25519.PrivateKey, manifest []byte) ([]byte, error) {
	pub, ok := priv.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("dist: not an ed25519 key")
	}
	return json.MarshalIndent(Signature{KeyID: KeyID(pub), Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(priv, manifest))}, "", "  ")
}

// LoadPrivateKey reads a base64 ed25519 seed (32 bytes) or private key (64).
func LoadPrivateKey(s string) (ed25519.PrivateKey, error) {
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, fmt.Errorf("dist: private key: %w", err)
	}
	switch len(b) {
	case ed25519.SeedSize:
		return ed25519.NewKeyFromSeed(b), nil
	case ed25519.PrivateKeySize:
		return ed25519.PrivateKey(b), nil
	}
	return nil, errors.New("dist: private key must be a 32-byte seed or 64-byte key (base64)")
}

// ReadLocal loads the installed manifest from an index dir (nil if absent).
func ReadLocal(dir string) (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("dist: local manifest: %w", err)
	}
	return &m, nil
}

// Files returns every local file name a manifest references.
func (m *Manifest) Files() []File {
	var out []File
	for _, s := range m.Shards {
		out = append(out, s.DB)
		out = append(out, s.Vectors...)
	}
	return append(out, m.Models...)
}

// ShardPaths lists the shard database paths of a manifest in dir.
func (m *Manifest) ShardPaths(dir string) []string {
	out := make([]string, 0, len(m.Shards))
	for _, s := range m.Shards {
		out = append(out, filepath.Join(dir, s.DB.Name))
	}
	return out
}

// Expired reports whether the manifest is past its expiry at t.
func (m *Manifest) Expired(t time.Time) bool { return !m.ExpiresAt.IsZero() && t.After(m.ExpiresAt) }

// safeName rejects names that could escape the index dir.
func safeName(n string) error {
	if n == "" || filepath.IsAbs(n) || strings.Contains(n, "..") || strings.ContainsAny(n, `\:`) {
		return fmt.Errorf("dist: unsafe file name %q in manifest", n)
	}
	return nil
}
