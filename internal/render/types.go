// Package render is the single renderer for every tool result
// (docs/mcp-surface.md §1): it turns engine results into the typed
// structuredContent value and a meaning-equivalent Markdown content block,
// enforcing token budgets, truncation markers, stateless cursors and the
// fencing of untrusted text. Tool handlers never format output themselves.
package render

// Citation is attached to every returned item (CLAUDE.md invariant 4).
type Citation struct {
	SourceURL   string `json:"source_url" jsonschema:"canonical URL of the upstream source"`
	Library     string `json:"library" jsonschema:"library id, e.g. wpilib, phoenix6, revlib"`
	Version     string `json:"version" jsonschema:"library version (or range) the content applies to"`
	UpstreamRev string `json:"upstream_rev" jsonschema:"upstream commit SHA, ETag or release tag the content was built from"`
	RetrievedAt string `json:"retrieved_at" jsonschema:"RFC 3339 time the content was fetched"`
	License     string `json:"license" jsonschema:"license of the upstream content"`
	Trust       string `json:"trust" jsonschema:"official | vendor | community; community text is untrusted data"`
	Suspect     bool   `json:"suspect,omitempty" jsonschema:"true when the content was flagged as a possible prompt injection"`
}

// Envelope is the common status block of every result.
type Envelope struct {
	Status     string   `json:"status" jsonschema:"ok | low_confidence | no_match | version_mismatch | syncing"`
	Confidence float64  `json:"confidence" jsonschema:"0..1 confidence in the top result"`
	Season     string   `json:"frc_season,omitempty" jsonschema:"effective FRC season the results are pinned to"`
	PinSource  string   `json:"pin_source,omitempty" jsonschema:"where the season came from: arg | query | default"`
	Language   string   `json:"language,omitempty" jsonschema:"effective language filter"`
	Freshness  string   `json:"freshness" jsonschema:"shard (served from the local index) | live"`
	IndexAge   string   `json:"index_age,omitempty" jsonschema:"age of the newest loaded index shard"`
	IndexStale bool     `json:"index_stale,omitempty" jsonschema:"true when the index manifest is past its expiry"`
	Truncated  bool     `json:"truncated" jsonschema:"true when results were cut to fit the token budget"`
	Omitted    int      `json:"omitted,omitempty" jsonschema:"number of results left out by truncation"`
	NextCursor string   `json:"next_cursor,omitempty" jsonschema:"pass back as cursor to get the next page"`
	Degraded   []string `json:"degraded,omitempty" jsonschema:"retrievers that were unavailable for this call"`
	Next       []string `json:"next,omitempty" jsonschema:"suggested next tool calls"`
}

// SearchHit is one frc_search result.
type SearchHit struct {
	ID          string   `json:"id" jsonschema:"stable chunk id; pass to frc_fetch for the full section"`
	Title       string   `json:"title"`
	HeadingPath string   `json:"heading_path,omitempty"`
	Kind        string   `json:"kind" jsonschema:"prose | code | api | release | rule | forum"`
	Library     string   `json:"library"`
	Version     string   `json:"version"`
	Season      string   `json:"frc_season"`
	Language    string   `json:"language"`
	Snippet     string   `json:"snippet"`
	Score       float64  `json:"score"`
	ExactSymbol bool     `json:"exact_symbol,omitempty" jsonschema:"true when the hit matched an API symbol exactly"`
	Citation    Citation `json:"citation"`
}

// SymbolRef summarizes an exact API symbol match.
type SymbolRef struct {
	FQN          string `json:"fqn"`
	Kind         string `json:"kind"`
	Signature    string `json:"signature"`
	Library      string `json:"library"`
	Version      string `json:"version"`
	Season       string `json:"frc_season"`
	Language     string `json:"language"`
	DeprecatedIn string `json:"deprecated_in,omitempty"`
	RemovedIn    string `json:"removed_in,omitempty"`
	Replacement  string `json:"replacement,omitempty"`
}

// SearchOut is frc_search's structured result.
type SearchOut struct {
	Envelope
	Hits            []SearchHit `json:"hits"`
	OtherSeasonHits []SearchHit `json:"other_season_hits,omitempty" jsonschema:"version_mismatch only: matches from other seasons — do not use for the pinned season without migrating"`
	Symbols         []SymbolRef `json:"symbols,omitempty" jsonschema:"exact API symbol matches; call frc_api for full detail"`
}

// FetchOut is frc_fetch's structured result.
type FetchOut struct {
	Envelope
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	HeadingPath string   `json:"heading_path,omitempty"`
	Kind        string   `json:"kind"`
	Library     string   `json:"library"`
	Version     string   `json:"version"`
	Season      string   `json:"frc_season"`
	Language    string   `json:"language"`
	Body        string   `json:"body" jsonschema:"section content (Markdown)"`
	Citation    Citation `json:"citation"`
}

// SymbolOut is one frc_api match.
type SymbolOut struct {
	FQN          string   `json:"fqn"`
	Kind         string   `json:"kind"`
	Signature    string   `json:"signature"`
	Summary      string   `json:"summary,omitempty"`
	Library      string   `json:"library"`
	Version      string   `json:"version"`
	Season       string   `json:"frc_season"`
	Language     string   `json:"language"`
	Since        string   `json:"since,omitempty"`
	DeprecatedIn string   `json:"deprecated_in,omitempty"`
	RemovedIn    string   `json:"removed_in,omitempty"`
	Replacement  string   `json:"replacement,omitempty"`
	DocID        string   `json:"doc_id,omitempty" jsonschema:"chunk id of the related documentation; pass to frc_fetch"`
	Citation     Citation `json:"citation"`
}

// APIOut is frc_api's structured result.
type APIOut struct {
	Envelope
	Matches      []SymbolOut `json:"matches"`
	OtherSeasons []SymbolOut `json:"other_seasons,omitempty" jsonschema:"the same symbol in other seasons (e.g. before/after a package move)"`
}

// VendordepOut is a detected vendordep.
type VendordepOut struct {
	File    string `json:"file"`
	Name    string `json:"name"`
	Version string `json:"version"`
	FRCYear string `json:"frc_year,omitempty"`
}

// ContextOut is frc_context's structured result.
type ContextOut struct {
	Envelope
	Pin        string            `json:"pin" jsonschema:"opaque handle; pass as pin to frc_search / frc_api to apply this project's season, language and versions"`
	Channel    string            `json:"channel,omitempty"`
	WPILib     string            `json:"wpilib_version,omitempty"`
	Libraries  map[string]string `json:"libraries,omitempty" jsonschema:"library name → version pinned by the project"`
	Vendordeps []VendordepOut    `json:"vendordeps"`
	Files      []string          `json:"files" jsonschema:"project files that were read"`
	Warnings   []string          `json:"warnings,omitempty" jsonschema:"compatibility problems found (e.g. vendordep frcYear does not match the WPILib season)"`
}
