package dist

import (
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// PublishOptions configures Publish.
type PublishOptions struct {
	InDir   string // shard dir produced by `frc-mcp index run`
	OutDir  string // where objects + manifest are written
	Channel string
	Prev    *Manifest // previous published manifest (for the serial); nil = first
	Key     ed25519.PrivateKey
	TTL     time.Duration // expiry window (default 30 days)
	Now     time.Time

	// IncludeUnlicensed publishes shards with content whose license is a
	// LicenseRef-* (the vendor publishes no license; docs/sources.md §0.2).
	// It never covers LicenseRef-*-UserContent (forum posts).
	// Off by default: such shards are built and usable locally, but only
	// redistributed once permission is recorded.
	IncludeUnlicensed bool
	// OnSkip is told about every shard left out of the manifest.
	OnSkip func(shard, reason string)
}

// Unlicensed reports whether a chunk license means "no redistribution grant".
func Unlicensed(license string) bool { return index.RestrictedLicense(license) }

// Publish packages every shard in InDir (with vector layers and models) into
// OutDir and writes a signed manifest. Object names are content-addressed, so
// unchanged files keep their name and clients skip them.
func Publish(ctx context.Context, o PublishOptions) (*Manifest, error) {
	if o.Now.IsZero() {
		o.Now = time.Now().UTC()
	}
	if o.TTL <= 0 {
		o.TTL = 30 * 24 * time.Hour
	}
	if err := os.MkdirAll(o.OutDir, 0o755); err != nil {
		return nil, err
	}
	m := &Manifest{Schema: ManifestSchema, Channel: o.Channel, CreatedAt: o.Now.Truncate(time.Second),
		ExpiresAt: o.Now.Add(o.TTL).Truncate(time.Second), IndexSchema: index.SchemaVersion, Serial: 1}
	if o.Prev != nil {
		m.Serial = o.Prev.Serial + 1
	}
	paths, err := filepath.Glob(filepath.Join(o.InDir, "*.sqlite"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	for _, p := range paths {
		r, err := index.Open(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("publish: %w", err)
		}
		meta := r.Meta()
		lics, err := r.Licenses(ctx)
		r.Close()
		if err != nil {
			return nil, fmt.Errorf("publish: %s: %w", p, err)
		}
		// Forum posts are other people's words, often minors', and some
		// vendor content is under a license that forbids distribution: never
		// redistributed, whatever the flags (docs/sources.md §0.2, §5).
		if no := slices.DeleteFunc(slices.Clone(lics), func(l string) bool { return !sources.Prohibited(l) }); len(no) > 0 {
			if o.OnSkip != nil {
				o.OnSkip(meta.Name, "content that is never redistributed ("+strings.Join(no, ", ")+"); build it locally")
			}
			continue
		}
		if bad := slices.DeleteFunc(lics, func(l string) bool { return !Unlicensed(l) }); len(bad) > 0 && !o.IncludeUnlicensed {
			if o.OnSkip != nil {
				o.OnSkip(meta.Name, "unlicensed content ("+strings.Join(bad, ", ")+"); pass --include-unlicensed once permission is recorded")
			}
			continue
		}
		sh := Shard{Name: meta.Name, BuildID: meta.BuildID}
		for _, sc := range meta.Seasons {
			sh.Seasons = append(sh.Seasons, sc.Season)
		}
		// Local names carry the build id so a new version never overwrites a
		// file a running server has open (Windows cannot replace open files).
		if sh.DB, err = pack(p, meta.Name+"."+meta.BuildID[:12]+".sqlite", o.OutDir); err != nil {
			return nil, err
		}
		vecs, _ := filepath.Glob(strings.TrimSuffix(p, ".sqlite") + ".*.vec")
		sort.Strings(vecs)
		for _, v := range vecs {
			model := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(v), meta.Name+"."), ".vec")
			f, err := pack(v, meta.Name+"."+meta.BuildID[:12]+"."+model+".vec", o.OutDir)
			if err != nil {
				return nil, err
			}
			sh.Vectors = append(sh.Vectors, f)
		}
		m.Shards = append(m.Shards, sh)
	}
	models, _ := filepath.Glob(filepath.Join(o.InDir, "models", "*", "*"))
	sort.Strings(models)
	for _, p := range models {
		rel, _ := filepath.Rel(o.InDir, p)
		f, err := pack(p, filepath.ToSlash(rel), o.OutDir)
		if err != nil {
			return nil, err
		}
		m.Models = append(m.Models, f)
	}
	if len(m.Shards) == 0 {
		return nil, fmt.Errorf("publish: no shards in %s", o.InDir)
	}
	body, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	sig, err := Sign(o.Key, body)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(o.OutDir, "manifest.json"), body, 0o644); err != nil {
		return nil, err
	}
	return m, os.WriteFile(filepath.Join(o.OutDir, "manifest.json.sig"), sig, 0o644)
}

// pack gzips src into outDir under a content-addressed object name.
func pack(src, localName, outDir string) (File, error) {
	in, err := os.Open(src)
	if err != nil {
		return File{}, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(outDir, ".pack-*")
	if err != nil {
		return File{}, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	raw, gz := sha256.New(), sha256.New()
	zw, _ := gzip.NewWriterLevel(io.MultiWriter(tmp, gz), gzip.BestCompression)
	zw.ModTime = time.Time{} // deterministic output
	n, err := io.Copy(io.MultiWriter(zw, raw), in)
	if err == nil {
		err = zw.Close()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return File{}, err
	}
	st, err := os.Stat(tmp.Name())
	if err != nil {
		return File{}, err
	}
	f := File{Name: localName, SHA256: hex.EncodeToString(raw.Sum(nil)), GzSHA256: hex.EncodeToString(gz.Sum(nil)),
		Size: n, GzSize: st.Size()}
	base := strings.ReplaceAll(filepath.Base(localName), ".", "-")
	f.Object = base + "-" + f.GzSHA256[:16] + ".gz"
	return f, os.Rename(tmp.Name(), filepath.Join(outDir, f.Object))
}
