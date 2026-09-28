package main

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/dist"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/netguard"
)

// syncClient builds a guarded client for a sync base URL (its host plus the
// GitHub Releases redirect hosts).
//
// allowPrivate permits a LAN mirror (school server on 10.x / 192.168.x). The
// allowlist still pins the host, and signatures are still verified, so the
// only thing relaxed is the dial-time public-address check.
func syncClient(base string, allowPrivate bool) (*http.Client, error) {
	u, err := url.Parse(base)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return nil, fmt.Errorf("sync url %q must be https", base)
	}
	hosts := append([]string{u.Hostname()}, dist.RedirectHosts...)
	return (&netguard.Policy{Hosts: hosts, UserAgent: "frc-mcp/" + buildVersion() + " (+https://github.com/fikretyukselit/frc-mcp)",
		MaxBody: 512 << 20, Timeout: 15 * time.Minute, AllowNonPublic: allowPrivate}).Client(), nil
}

func syncCmd(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("sync", flag.ContinueOnError)
	dir := fs.String("index", index.DefaultDir(), "local index directory")
	base := fs.String("url", dist.DefaultSyncURL, "index channel base URL")
	key := fs.String("trusted-key", "", "additional trusted ed25519 public key (base64), e.g. for a self-hosted mirror")
	verifyOnly := fs.Bool("verify", false, "only re-verify the installed index (signature + digests)")
	allowPrivate := fs.Bool("allow-private", false, "allow a mirror on a private/LAN address (signatures are still verified)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	kr, err := dist.TrustedKeyring(*key)
	if err != nil {
		return err
	}
	if *verifyOnly {
		if err := dist.VerifyLocal(*dir, kr); err != nil {
			return err
		}
		fmt.Println("installed index: signature and digests OK")
		return nil
	}
	c, err := syncClient(*base, *allowPrivate)
	if err != nil {
		return err
	}
	res, err := dist.Sync(ctx, dist.SyncOptions{BaseURL: *base, Dir: *dir, Keyring: kr, Client: c,
		Progress: func(done, total int64) { fmt.Fprintf(os.Stderr, "\r%5.1f%%", 100*float64(done)/float64(max(total, 1))) }})
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr)
	switch {
	case res.Updated:
		fmt.Printf("index updated to serial %d: %d file(s), %.1f MB downloaded\n", res.Serial, res.Downloaded, float64(res.Bytes)/1e6)
	default:
		fmt.Printf("index already at serial %d\n", res.Serial)
	}
	if res.Expired {
		fmt.Println("warning: the published manifest is past its expiry; the publisher may be stalled")
	}
	return nil
}

func indexPublish(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("index publish", flag.ContinueOnError)
	in := fs.String("in", ".shards", "shard directory built by `index run`")
	out := fs.String("out", "dist/index", "output directory for objects + manifest")
	channel := fs.String("channel", "stable", "channel name")
	prev := fs.String("prev", "", "previous manifest.json (for the monotonic serial); empty = serial 1")
	keyEnv := fs.String("key-env", "FRC_MCP_INDEX_KEY", "environment variable holding the base64 ed25519 private key")
	ttl := fs.Duration("ttl", 30*24*time.Hour, "manifest validity")
	if err := fs.Parse(args); err != nil {
		return err
	}
	priv, err := dist.LoadPrivateKey(os.Getenv(*keyEnv))
	if err != nil {
		return fmt.Errorf("%w (set %s)", err, *keyEnv)
	}
	var pm *dist.Manifest
	if *prev != "" {
		b, err := os.ReadFile(*prev)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err == nil {
			pm = &dist.Manifest{}
			if err := json.Unmarshal(b, pm); err != nil {
				return fmt.Errorf("previous manifest: %w", err)
			}
		}
	}
	m, err := dist.Publish(ctx, dist.PublishOptions{InDir: *in, OutDir: *out, Channel: *channel, Prev: pm, Key: priv, TTL: *ttl})
	if err != nil {
		return err
	}
	fmt.Printf("published serial %d (%d shards, %d model files) to %s, key %s, expires %s\n",
		m.Serial, len(m.Shards), len(m.Models), *out, dist.KeyID(priv.Public().(ed25519.PublicKey)), m.ExpiresAt.Format(time.RFC3339))
	return nil
}

// indexKeygen creates a signing key pair. The private key must go straight
// into a CI secret; it is printed once and never written to disk.
func indexKeygen() error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Printf("public key (add to internal/dist/keys.go trustedKeys):\n  %s\nkey id: %s\n\nprivate key seed (store ONLY as the CI secret FRC_MCP_INDEX_KEY; it is not saved anywhere):\n  %s\n",
		base64.StdEncoding.EncodeToString(pub), dist.KeyID(pub), base64.StdEncoding.EncodeToString(priv.Seed()))
	return nil
}
