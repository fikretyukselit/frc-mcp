// Package index defines the frc-mcp shard format: an immutable SQLite file that
// holds chunk text with provenance, an FTS5 index over it, and API symbol tables.
//
// Shards are written once by the ingestion plane (Writer) and opened read-only
// and immutable by the serving plane (Reader). See docs/architecture.md §5.
package index

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// SchemaVersion is the shard schema major version. Readers refuse shards whose
// major version differs; any incompatible change to the DDL must bump it.
const SchemaVersion = 2

// Enumerated field values. They are stored as text so shards stay inspectable
// with the sqlite3 CLI.
var (
	Channels  = []string{"stable", "beta", "alpha"}
	Languages = []string{"java", "cpp", "python", "any"}
	Kinds     = []string{"prose", "code", "api", "release", "rule", "forum"}
	Trusts    = []string{"official", "vendor", "community"}
)

// ErrInvalidChunk is returned (wrapped) when a chunk violates the provenance
// invariant (CLAUDE.md §5.1). The writer fails instead of indexing it.
var ErrInvalidChunk = errors.New("index: invalid chunk")

// Chunk is the unit of retrieval. Every field except Title, HeadingPath,
// Symbol, Anchor, VersionHi and Suspect is mandatory.
type Chunk struct {
	DocID       string    `json:"doc_id"`
	Ord         int       `json:"ord"`
	Library     string    `json:"library"`
	VersionLo   string    `json:"version_lo"`
	VersionHi   string    `json:"version_hi,omitempty"`
	Season      string    `json:"season"`
	Channel     string    `json:"channel"`
	Language    string    `json:"language"`
	Kind        string    `json:"kind"`
	Title       string    `json:"title,omitempty"`
	HeadingPath string    `json:"heading_path,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	Prefix      string    `json:"prefix,omitempty"`
	Body        string    `json:"body"`
	SourceURL   string    `json:"source_url"`
	Anchor      string    `json:"anchor,omitempty"`
	UpstreamRev string    `json:"upstream_rev"`
	RetrievedAt time.Time `json:"retrieved_at"`
	License     string    `json:"license"`
	Trust       string    `json:"trust"`
	Suspect     bool      `json:"suspect,omitempty"`
	Authority   int       `json:"authority,omitempty"`
	Tokens      int       `json:"tokens,omitempty"`

	// DocNum is assigned by the shard writer (dense per shard); ignored on input.
	DocNum int32 `json:"-"`
}

// ID is the stable, human-readable chunk identifier used by frc_search hits and
// accepted by frc_fetch: "<doc_id>#<ord>".
func (c *Chunk) ID() string { return c.DocID + "#" + strconv.Itoa(c.Ord) }

// ParseID splits an ID produced by [Chunk.ID].
func ParseID(id string) (docID string, ord int, err error) {
	i := strings.LastIndexByte(id, '#')
	if i <= 0 || i == len(id)-1 {
		return "", 0, fmt.Errorf("malformed chunk id %q: want <doc_id>#<ord>", id)
	}
	ord, err = strconv.Atoi(id[i+1:])
	if err != nil || ord < 0 {
		return "", 0, fmt.Errorf("malformed chunk id %q: ordinal must be a non-negative integer", id)
	}
	return id[:i], ord, nil
}

// Version renders the chunk's version range for display.
func (c *Chunk) Version() string {
	if c.VersionHi == "" || c.VersionHi == c.VersionLo {
		return c.VersionLo
	}
	return c.VersionLo + "–" + c.VersionHi
}

// Validate enforces the provenance invariant.
func (c *Chunk) Validate() error {
	required := [...]struct{ name, v string }{
		{"doc_id", c.DocID}, {"library", c.Library}, {"version_lo", c.VersionLo},
		{"season", c.Season}, {"body", c.Body}, {"source_url", c.SourceURL},
		{"upstream_rev", c.UpstreamRev}, {"license", c.License},
	}
	for _, f := range required {
		if strings.TrimSpace(f.v) == "" {
			return fmt.Errorf("%w %s: missing %s", ErrInvalidChunk, c.ID(), f.name)
		}
	}
	if c.Ord < 0 {
		return fmt.Errorf("%w %s: negative ord", ErrInvalidChunk, c.ID())
	}
	if c.RetrievedAt.IsZero() {
		return fmt.Errorf("%w %s: missing retrieved_at", ErrInvalidChunk, c.ID())
	}
	enums := [...]struct {
		name, v string
		set     []string
	}{
		{"channel", c.Channel, Channels}, {"language", c.Language, Languages},
		{"kind", c.Kind, Kinds}, {"trust", c.Trust, Trusts},
	}
	for _, f := range enums {
		if !contains(f.set, f.v) {
			return fmt.Errorf("%w %s: %s=%q not in %v", ErrInvalidChunk, c.ID(), f.name, f.v, f.set)
		}
	}
	if !strings.HasPrefix(c.SourceURL, "https://") {
		return fmt.Errorf("%w %s: source_url must be https", ErrInvalidChunk, c.ID())
	}
	return nil
}

// Symbol is one API element (type, method overload group, field, …) in a
// specific library version and language. Symbols are exact facts: they are
// served by lookup, never by similarity search.
type Symbol struct {
	FQN          string `json:"fqn"`
	Library      string `json:"library"`
	Version      string `json:"version"`
	Season       string `json:"season"`
	Language     string `json:"language"`
	Kind         string `json:"kind"` // class|interface|enum|method|constructor|field|function|namespace
	Signature    string `json:"signature"`
	Summary      string `json:"summary,omitempty"`
	Since        string `json:"since,omitempty"`
	DeprecatedIn string `json:"deprecated_in,omitempty"`
	RemovedIn    string `json:"removed_in,omitempty"`
	Replacement  string `json:"replacement,omitempty"`
	// ReplacementSrc says where Replacement came from: "upstream" (the
	// library's own deprecation note), "generated" (apisym.Diff across
	// seasons) or "curated" (data/migrations, with a citation).
	ReplacementSrc string    `json:"replacement_src,omitempty"`
	ChunkID        string    `json:"chunk_id,omitempty"`
	SourceURL      string    `json:"source_url"`
	UpstreamRev    string    `json:"upstream_rev"`
	RetrievedAt    time.Time `json:"retrieved_at"`
	License        string    `json:"license"`
	Trust          string    `json:"trust"`
	// CaseMismatch marks a lookup result whose name matches the query only
	// case-insensitively (set by Reader.Symbols, never stored).
	CaseMismatch bool `json:"-"`
}

// SimpleName returns the last path element of an FQN: the member name for
// "pkg.Type#member", otherwise the type name for "pkg.Type" / "ns::Type".
func SimpleName(fqn string) string {
	if i := strings.LastIndexByte(fqn, '#'); i >= 0 {
		return fqn[i+1:]
	}
	return lastSegment(fqn)
}

// OwnerName returns the simple name of the declaring type for a member FQN
// ("pkg.Type#member" → "Type"), or "" for non-members.
func OwnerName(fqn string) string {
	i := strings.LastIndexByte(fqn, '#')
	if i < 0 {
		return ""
	}
	return lastSegment(fqn[:i])
}

func lastSegment(s string) string {
	if i := strings.LastIndex(s, "::"); i >= 0 {
		s = s[i+2:]
	}
	if i := strings.LastIndexByte(s, '.'); i >= 0 {
		s = s[i+1:]
	}
	return s
}

// Validate enforces the provenance invariant for symbols.
func (s *Symbol) Validate() error {
	required := [...]struct{ name, v string }{
		{"fqn", s.FQN}, {"library", s.Library}, {"version", s.Version}, {"season", s.Season},
		{"kind", s.Kind}, {"signature", s.Signature}, {"source_url", s.SourceURL},
		{"upstream_rev", s.UpstreamRev}, {"license", s.License},
	}
	for _, f := range required {
		if strings.TrimSpace(f.v) == "" {
			return fmt.Errorf("%w symbol %s: missing %s", ErrInvalidChunk, s.FQN, f.name)
		}
	}
	if !contains(Languages, s.Language) || s.Language == "any" {
		return fmt.Errorf("%w symbol %s: language=%q must be java, cpp or python", ErrInvalidChunk, s.FQN, s.Language)
	}
	if !contains(Trusts, s.Trust) {
		return fmt.Errorf("%w symbol %s: trust=%q not in %v", ErrInvalidChunk, s.FQN, s.Trust, Trusts)
	}
	if s.RetrievedAt.IsZero() {
		return fmt.Errorf("%w symbol %s: missing retrieved_at", ErrInvalidChunk, s.FQN)
	}
	return nil
}

func contains(set []string, v string) bool {
	for _, s := range set {
		if s == v {
			return true
		}
	}
	return false
}

// RestrictedLicense reports whether a license id is a LicenseRef-*: content
// whose publisher grants no redistribution license (docs/sources.md §0.2).
// It is never published nor served by a hosted server.
func RestrictedLicense(license string) bool { return strings.HasPrefix(license, "LicenseRef-") }
