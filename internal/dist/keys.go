package dist

import (
	"os"
)

// DefaultSyncURL is the official index channel (GitHub Releases, rolling tag
// per channel). It is reachable once the repository is public.
const DefaultSyncURL = "https://github.com/fikretyukselit/frc-mcp/releases/download/index-stable"

// RedirectHosts are the hosts GitHub Releases downloads redirect to.
var RedirectHosts = []string{"github.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com"}

// trustedKeys are the official index signing keys (base64 ed25519 public
// keys). Rotation: add the new key here one release before switching the
// publisher, remove the old one a release after.
//
// 4d6c685f407388a5: production key, generated 2026-09-29 (ADR-0006 rollout);
// its seed exists only as FRC_MCP_INDEX_KEY in the protected index-publish
// environment.
var trustedKeys = []string{
	"4Puxgh/jMb9e9yNTdYVHe20jwUFuHdgEJoZEGL4SF74=",
}

// TrustedKeyring returns the compiled-in keys plus FRC_MCP_TRUSTED_KEYS
// (for self-hosted mirrors that sign with their own key).
func TrustedKeyring(extra string) (Keyring, error) {
	kr := Keyring{}
	for _, src := range append(append([]string{}, trustedKeys...), extra, os.Getenv("FRC_MCP_TRUSTED_KEYS")) {
		if src == "" {
			continue
		}
		k, err := ParseKeyring(src)
		if err != nil {
			return nil, err
		}
		for id, pub := range k {
			kr[id] = pub
		}
	}
	return kr, nil
}
