package dist

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/netguard"
)

// SyncOptions configures Sync.
type SyncOptions struct {
	BaseURL  string       // e.g. https://github.com/<org>/frc-mcp/releases/download/index-stable
	Dir      string       // local index dir
	Keyring  Keyring      // trusted signing keys
	Client   *http.Client // guarded client (netguard)
	Progress func(done, total int64)
}

// SyncResult reports what changed.
type SyncResult struct {
	Updated    bool
	Serial     int64
	Downloaded int
	Bytes      int64
	Expired    bool
}

// Sync brings dir up to the published manifest. It is safe to run while a
// server reads the previous version: new files get new names, and the local
// manifest (the commit point) is replaced atomically last.
func Sync(ctx context.Context, o SyncOptions) (*SyncResult, error) {
	if len(o.Keyring) == 0 {
		return nil, errors.New("dist: no trusted signing keys configured; refusing to install an unverifiable index")
	}
	base := strings.TrimSuffix(o.BaseURL, "/")
	body, err := get(ctx, o.Client, base+"/manifest.json", 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := get(ctx, o.Client, base+"/manifest.json.sig", 16<<10)
	if err != nil {
		return nil, err
	}
	if err := o.Keyring.Verify(body, sig); err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("dist: manifest: %w", err)
	}
	if m.Schema != ManifestSchema || m.IndexSchema != index.SchemaVersion {
		return nil, fmt.Errorf("%w (manifest v%d / index v%d; this binary: v%d / v%d)", ErrSchema, m.Schema, m.IndexSchema, ManifestSchema, index.SchemaVersion)
	}
	local, err := ReadLocal(o.Dir)
	if err != nil {
		return nil, err
	}
	res := &SyncResult{Serial: m.Serial, Expired: m.Expired(time.Now())}
	if local != nil && m.Serial < local.Serial {
		return nil, fmt.Errorf("%w: published %d < installed %d", ErrRollback, m.Serial, local.Serial)
	}
	if local != nil && m.Serial == local.Serial {
		return res, nil
	}
	if err := os.MkdirAll(o.Dir, 0o755); err != nil {
		return nil, err
	}
	files := m.Files()
	var total int64
	for _, f := range files {
		if err := safeName(f.Name); err != nil {
			return nil, err
		}
		if err := safeName(f.Object); err != nil {
			return nil, err
		}
		total += f.GzSize
	}
	var done int64
	for _, f := range files {
		dst := filepath.Join(o.Dir, filepath.FromSlash(f.Name))
		if fileSHA(dst) == f.SHA256 {
			done += f.GzSize
			continue // content-addressed: unchanged
		}
		n, err := fetchFile(ctx, o.Client, base+"/"+f.Object, dst, f)
		if err != nil {
			return nil, err
		}
		res.Downloaded++
		res.Bytes += n
		done += f.GzSize
		if o.Progress != nil {
			o.Progress(done, total)
		}
	}
	// Commit: the manifest (with its signature) switches the active set.
	if err := writeAtomic(filepath.Join(o.Dir, "manifest.json.sig"), sig); err != nil {
		return nil, err
	}
	if err := writeAtomic(filepath.Join(o.Dir, "manifest.json"), body); err != nil {
		return nil, err
	}
	prune(o.Dir, &m)
	res.Updated = true
	return res, nil
}

// VerifyLocal re-checks the installed manifest signature and file digests
// (frc-mcp doctor / tamper detection).
func VerifyLocal(dir string, kr Keyring) error {
	body, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	sig, err := os.ReadFile(filepath.Join(dir, "manifest.json.sig"))
	if err != nil {
		return err
	}
	if err := kr.Verify(body, sig); err != nil {
		return err
	}
	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return err
	}
	for _, f := range m.Files() {
		if fileSHA(filepath.Join(dir, filepath.FromSlash(f.Name))) != f.SHA256 {
			return fmt.Errorf("%w: %s", ErrDigest, f.Name)
		}
	}
	return nil
}

func get(ctx context.Context, c *http.Client, url string, limit int64) ([]byte, error) {
	resp, err := netguard.Get(ctx, c, url, nil)
	if err != nil {
		return nil, fmt.Errorf("dist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("dist: GET %s: HTTP %d", url, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// fetchFile downloads a gzip object, verifying both digests, into dst.
func fetchFile(ctx context.Context, c *http.Client, url, dst string, f File) (int64, error) {
	resp, err := netguard.Get(ctx, c, url, nil)
	if err != nil {
		return 0, fmt.Errorf("dist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("dist: GET %s: HTTP %d", url, resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	gzHash := sha256.New()
	// Both sides are bounded by the signed manifest: the compressed stream by
	// GzSize and the decompressed output by Size (+1 to detect overflow), so
	// a decompression bomb cannot exceed what the signer declared.
	compressed := io.TeeReader(io.LimitReader(resp.Body, f.GzSize+1), gzHash)
	zr, err := gzip.NewReader(compressed)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %w", ErrDigest, f.Name, err)
	}
	tmp := dst + ".part"
	out, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	raw := sha256.New()
	n, err := io.Copy(io.MultiWriter(out, raw), io.LimitReader(zr, f.Size+1))
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	_, _ = io.Copy(io.Discard, compressed) // drain the (bounded) compressed tail so the gzip hash covers the object
	if err == nil && (n != f.Size || hex.EncodeToString(raw.Sum(nil)) != f.SHA256 || hex.EncodeToString(gzHash.Sum(nil)) != f.GzSHA256) {
		err = fmt.Errorf("%w: %s", ErrDigest, f.Name)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return 0, err
	}
	return f.GzSize, os.Rename(tmp, dst)
}

func fileSHA(path string) string {
	fh, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer fh.Close()
	h := sha256.New()
	if _, err := io.Copy(h, fh); err != nil {
		return ""
	}
	return hex.EncodeToString(h.Sum(nil))
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// prune removes shard/vector files not referenced by m (best effort: files a
// running server still has open are skipped on platforms that forbid it).
func prune(dir string, m *Manifest) {
	keep := map[string]bool{}
	for _, f := range m.Files() {
		keep[filepath.Clean(filepath.Join(dir, filepath.FromSlash(f.Name)))] = true
	}
	for _, pat := range []string{"*.sqlite", "*.vec", "*.part"} {
		ps, _ := filepath.Glob(filepath.Join(dir, pat))
		for _, p := range ps {
			if !keep[filepath.Clean(p)] {
				_ = os.Remove(p)
			}
		}
	}
}
