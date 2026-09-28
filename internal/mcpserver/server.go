// Package mcpserver wires the retrieval engine to the MCP protocol: tool,
// resource and prompt registration, argument validation with instructive
// errors, and rendering through internal/render. It holds no business logic.
package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/mcpserver/surface"
	"github.com/fikretyukselit/frc-mcp/internal/render"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// Server is the frc-mcp MCP server. The engine can be swapped atomically
// (index sync) while requests are in flight.
type Server struct {
	mcp    *mcp.Server
	engine atomic.Pointer[retrieve.Engine]
	now    func() time.Time
}

// Options configures the server.
type Options struct {
	Version string
	Now     func() time.Time // for deterministic tests
}

// Limits and enumerations for argument validation.
const (
	maxQueryLen  = 2000
	maxSymbolLen = 300
	maxK         = 20
	cacheTTL     = time.Hour
)

var (
	languages = []string{"java", "cpp", "python", "any"}
	channels  = []string{"stable", "beta", "alpha"}
	kinds     = index.Kinds
	formats   = []string{"concise", "detailed"}
	seasonRe  = regexp.MustCompile(`^20[2-3][0-9]$`)
)

// New builds the server. engine may be nil (index not yet synced): tools then
// answer with status "syncing".
func New(engine *retrieve.Engine, opt Options) *Server {
	if opt.Version == "" {
		opt.Version = "dev"
	}
	s := &Server{now: opt.Now}
	if s.now == nil {
		s.now = time.Now
	}
	s.engine.Store(engine)
	s.mcp = mcp.NewServer(&mcp.Implementation{Name: "frc-mcp", Title: "FRC MCP", Version: opt.Version}, &mcp.ServerOptions{
		Instructions: surface.Instructions(),
		// Logging is deprecated (2026-07-28); advertise no default capabilities.
		Capabilities: &mcp.ServerCapabilities{},
		SetCacheable: func(_ context.Context, _ mcp.Request, c *mcp.Cacheable) {
			if c.TTLMs == 0 {
				c.TTLMs, c.CacheScope = int(cacheTTL.Milliseconds()), "public"
			}
		},
	})
	s.register()
	return s
}

// MCP returns the underlying SDK server (for transports).
func (s *Server) MCP() *mcp.Server { return s.mcp }

// SetEngine atomically replaces the engine (after an index sync).
func (s *Server) SetEngine(e *retrieve.Engine) { s.engine.Store(e) }

func readOnly(title string) *mcp.ToolAnnotations {
	f := false
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, IdempotentHint: true, DestructiveHint: &f, OpenWorldHint: &f}
}

// register adds the tools. The SDK lists them sorted by name, which keeps
// tools/list deterministic (SEP-2549) and prompt-cache friendly.
func (s *Server) register() {
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_search", Title: "Search FRC docs", Description: surface.Search(),
		Annotations: readOnly("Search FRC docs"), InputSchema: searchSchema()}, s.search)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_fetch", Title: "Fetch FRC doc section", Description: surface.Fetch(),
		Annotations: readOnly("Fetch FRC doc section"), InputSchema: fetchSchema()}, s.fetch)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_api", Title: "Look up FRC API symbol", Description: surface.API(),
		Annotations: readOnly("Look up FRC API symbol"), InputSchema: apiSchema()}, s.api)
	s.mcp.AddResource(&mcp.Resource{URI: "frc://index/manifest", Name: "index-manifest", Title: "Loaded index shards",
		Description: "Shards currently loaded: names, build ids, build times, seasons, chunk and symbol counts.",
		MIMEType:    "application/json"}, s.manifest)
}

// ---- frc_search ----

// SearchIn is frc_search's input.
type SearchIn struct {
	Query          string   `json:"query" jsonschema:"what you are looking for, in natural language or with exact class/method names"`
	Season         string   `json:"frc_season,omitempty" jsonschema:"FRC season to pin results to, e.g. 2026. Defaults to the current stable season"`
	Channel        string   `json:"channel,omitempty" jsonschema:"release channel of the pinned season"`
	Language       string   `json:"language,omitempty" jsonschema:"robot code language; 'any' disables the filter"`
	Libraries      []string `json:"libraries,omitempty" jsonschema:"restrict to library ids, e.g. wpilib, phoenix6, revlib, photonvision, pathplannerlib"`
	Kinds          []string `json:"kinds,omitempty" jsonschema:"restrict content kinds; include forum to opt in to community posts"`
	K              int      `json:"k,omitempty" jsonschema:"number of results per page (default 5)"`
	ResponseFormat string   `json:"response_format,omitempty" jsonschema:"concise (default) or detailed snippets"`
	MaxTokens      int      `json:"max_tokens,omitempty" jsonschema:"token budget for the whole response (default 2500, max 20000)"`
	Cursor         string   `json:"cursor,omitempty" jsonschema:"next_cursor from a previous frc_search call with the same arguments"`
}

func (s *Server) search(ctx context.Context, _ *mcp.CallToolRequest, in SearchIn) (*mcp.CallToolResult, render.SearchOut, error) {
	in.Query = strings.TrimSpace(in.Query)
	if err := validateSearch(&in); err != nil {
		return nil, render.SearchOut{}, err
	}
	e := s.engine.Load()
	if !e.Ready() {
		out := render.SearchOut{Envelope: syncing(), Hits: []render.SearchHit{}}
		return text(render.SearchMarkdown(out)), out, nil
	}
	key := render.QueryKey(in.Query, in.Season, in.Channel, in.Language, strings.Join(in.Libraries, ","),
		strings.Join(in.Kinds, ","), in.ResponseFormat)
	offset := 0
	if in.Cursor != "" {
		var err error
		if offset, err = render.DecodeCursor(in.Cursor, key, e.Digest()); err != nil {
			return nil, render.SearchOut{}, cursorError(err, "frc_search")
		}
	}
	res, err := e.Search(ctx, retrieve.Query{Text: in.Query, Season: in.Season, Channel: in.Channel,
		Language: in.Language, Libraries: in.Libraries, Kinds: in.Kinds, Offset: offset, Limit: in.K})
	if err != nil {
		return nil, render.SearchOut{}, fmt.Errorf("search failed: %w", err)
	}
	out := render.Search(res, render.Context{Now: s.now(), BuiltAt: e.BuiltAt(), Digest: e.Digest(),
		Format: in.ResponseFormat, MaxTok: in.MaxTokens, Offset: offset, Limit: in.K, QueryKey: key})
	return text(render.SearchMarkdown(out)), out, nil
}

func validateSearch(in *SearchIn) error {
	if in.Query == "" {
		return errors.New(`query is required. Example: {"query": "How do I configure Motion Magic on a TalonFX?"}`)
	}
	if len(in.Query) > maxQueryLen {
		return fmt.Errorf("query is %d characters; the limit is %d. Ask one focused question per call", len(in.Query), maxQueryLen)
	}
	if err := validateCommon(in.Season, in.Channel, in.Language); err != nil {
		return err
	}
	for _, k := range in.Kinds {
		if !slices.Contains(kinds, k) {
			return fmt.Errorf("kinds contains %q; valid values: %s", k, strings.Join(kinds, ", "))
		}
	}
	if in.ResponseFormat != "" && !slices.Contains(formats, in.ResponseFormat) {
		return fmt.Errorf("response_format %q is invalid; valid values: concise, detailed", in.ResponseFormat)
	}
	switch {
	case in.K == 0:
		in.K = 5
	case in.K < 0 || in.K > maxK:
		return fmt.Errorf("k=%d is out of range; use 1–%d and page with next_cursor", in.K, maxK)
	}
	if in.MaxTokens < 0 || in.MaxTokens > render.HardMaxTokens {
		return fmt.Errorf("max_tokens=%d is out of range; use 1–%d", in.MaxTokens, render.HardMaxTokens)
	}
	return nil
}

func validateCommon(season, channel, language string) error {
	if season != "" && !seasonRe.MatchString(season) {
		return fmt.Errorf("frc_season %q is invalid; use a four-digit season year such as \"2026\" or \"2027\"", season)
	}
	if channel != "" && !slices.Contains(channels, channel) {
		return fmt.Errorf("channel %q is invalid; valid values: %s", channel, strings.Join(channels, ", "))
	}
	if language != "" && !slices.Contains(languages, language) {
		return fmt.Errorf("language %q is invalid; valid values: %s", language, strings.Join(languages, ", "))
	}
	return nil
}

// ---- frc_fetch ----

// FetchIn is frc_fetch's input.
type FetchIn struct {
	ID        string `json:"id,omitempty" jsonschema:"chunk id from frc_search, e.g. phoenix6/2026/motion-magic#0"`
	URI       string `json:"uri,omitempty" jsonschema:"alternative to id: frc://chunk/<id>"`
	MaxTokens int    `json:"max_tokens,omitempty" jsonschema:"token budget (default 2500, max 20000)"`
	Cursor    string `json:"cursor,omitempty" jsonschema:"next_cursor from a previous frc_fetch of the same id"`
}

func (s *Server) fetch(ctx context.Context, _ *mcp.CallToolRequest, in FetchIn) (*mcp.CallToolResult, render.FetchOut, error) {
	id, err := fetchID(in)
	if err != nil {
		return nil, render.FetchOut{}, err
	}
	if in.MaxTokens < 0 || in.MaxTokens > render.HardMaxTokens {
		return nil, render.FetchOut{}, fmt.Errorf("max_tokens=%d is out of range; use 1–%d", in.MaxTokens, render.HardMaxTokens)
	}
	e := s.engine.Load()
	if !e.Ready() {
		out := render.FetchOut{Envelope: syncing()}
		return text(render.FetchMarkdown(out)), out, nil
	}
	key := render.QueryKey("fetch", id)
	offset := 0
	if in.Cursor != "" {
		if offset, err = render.DecodeCursor(in.Cursor, key, e.Digest()); err != nil {
			return nil, render.FetchOut{}, cursorError(err, "frc_fetch")
		}
	}
	c, err := e.Fetch(ctx, id)
	if errors.Is(err, index.ErrNotFound) {
		return nil, render.FetchOut{}, fmt.Errorf("no section with id %q in the loaded index (it may have been re-indexed); run frc_search again to get current ids", id)
	}
	if err != nil {
		return nil, render.FetchOut{}, err
	}
	out := render.Fetch(c, render.Context{Now: s.now(), BuiltAt: e.BuiltAt(), Digest: e.Digest(),
		MaxTok: in.MaxTokens, Offset: offset, QueryKey: key})
	return text(render.FetchMarkdown(out)), out, nil
}

func fetchID(in FetchIn) (string, error) {
	id := strings.TrimSpace(in.ID)
	if u := strings.TrimSpace(in.URI); u != "" {
		rest, ok := strings.CutPrefix(u, "frc://chunk/")
		if !ok {
			return "", fmt.Errorf("uri %q is not supported; only frc://chunk/<id> URIs are accepted (no arbitrary URLs are fetched)", u)
		}
		if id != "" && id != rest {
			return "", errors.New("pass either id or uri, not both")
		}
		id = rest
	}
	if id == "" {
		return "", errors.New(`id is required: use an id from frc_search results, e.g. {"id": "phoenix6/2026/motion-magic#0"}`)
	}
	if _, _, err := index.ParseID(id); err != nil {
		return "", fmt.Errorf("%w; ids look like <doc_id>#<n> and come from frc_search results", err)
	}
	return id, nil
}

// ---- frc_api ----

// APIIn is frc_api's input.
type APIIn struct {
	Symbol   string `json:"symbol" jsonschema:"class, member or qualified name: TalonFX, SparkMax#configure, frc::DCMotor, edu.wpi.first.wpilibj.TimedRobot"`
	Season   string `json:"frc_season,omitempty" jsonschema:"FRC season, e.g. 2026. Defaults to the current stable season"`
	Language string `json:"language,omitempty" jsonschema:"java, cpp or python; empty searches all languages"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum matches (default 10, max 50)"`
}

func (s *Server) api(ctx context.Context, _ *mcp.CallToolRequest, in APIIn) (*mcp.CallToolResult, render.APIOut, error) {
	in.Symbol = strings.TrimSpace(in.Symbol)
	switch {
	case in.Symbol == "":
		return nil, render.APIOut{}, errors.New(`symbol is required. Example: {"symbol": "TalonFX", "language": "java"}`)
	case len(in.Symbol) > maxSymbolLen:
		return nil, render.APIOut{}, fmt.Errorf("symbol is longer than %d characters; pass a single class or member name", maxSymbolLen)
	case in.Language == "any":
		in.Language = ""
	}
	if err := validateCommon(in.Season, "", in.Language); err != nil {
		return nil, render.APIOut{}, err
	}
	if in.Limit == 0 {
		in.Limit = 10
	} else if in.Limit < 0 || in.Limit > 50 {
		return nil, render.APIOut{}, fmt.Errorf("limit=%d is out of range; use 1–50", in.Limit)
	}
	e := s.engine.Load()
	if !e.Ready() {
		out := render.APIOut{Envelope: syncing(), Matches: []render.SymbolOut{}}
		return text(render.APIMarkdown(out)), out, nil
	}
	season := in.Season
	if season == "" {
		season = e.DefaultSeason()
	}
	matches, err := e.Symbols(ctx, index.SymbolQuery{Name: in.Symbol, Season: season, Language: in.Language, Limit: in.Limit})
	if err != nil {
		return nil, render.APIOut{}, err
	}
	all, err := e.Symbols(ctx, index.SymbolQuery{Name: in.Symbol, Language: in.Language, Limit: in.Limit})
	if err != nil {
		return nil, render.APIOut{}, err
	}
	others := slices.DeleteFunc(all, func(x index.Symbol) bool { return x.Season == season })
	out := render.API(matches, others[:min(len(others), 5)], render.Context{Now: s.now(), BuiltAt: e.BuiltAt()}, season)
	out.PinSource = "default"
	if in.Season != "" {
		out.PinSource = "arg"
	}
	out.Language = in.Language
	return text(render.APIMarkdown(out)), out, nil
}

// ---- resources ----

func (s *Server) manifest(context.Context, *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
	e := s.engine.Load()
	type shard = index.Meta
	body := struct {
		Ready         bool    `json:"ready"`
		Digest        string  `json:"digest,omitempty"`
		DefaultSeason string  `json:"default_season,omitempty"`
		Shards        []shard `json:"shards"`
	}{Ready: e.Ready(), Shards: []shard{}}
	if e.Ready() {
		body.Digest, body.DefaultSeason, body.Shards = e.Digest(), e.DefaultSeason(), e.Metas()
	}
	b, _ := json.MarshalIndent(body, "", "  ")
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{URI: "frc://index/manifest",
		MIMEType: "application/json", Text: string(b)}}}, nil
}

// ---- helpers ----

func text(md string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: md}}}
}

func syncing() render.Envelope {
	return render.Envelope{Status: retrieve.StatusSyncing, Freshness: "shard",
		Next: []string{"the FRC index is not available yet; run `frc-mcp index build` (M0) / `frc-mcp sync`, then retry"}}
}

func cursorError(err error, tool string) error {
	if errors.Is(err, render.ErrStaleCursor) {
		return fmt.Errorf("cursor does not match these arguments or the index was updated; call %s again without cursor", tool)
	}
	return fmt.Errorf("invalid cursor (%w); pass next_cursor exactly as returned", err)
}

// ---- input schemas: inferred from the structs, then tightened ----

func searchSchema() *jsonschema.Schema {
	s := infer[SearchIn]()
	enum(s, "language", languages)
	enum(s, "channel", channels)
	enum(s, "response_format", formats)
	s.Properties["kinds"].Items.Enum = toAny(kinds)
	s.Properties["frc_season"].Pattern = seasonRe.String()
	s.Properties["query"].MaxLength = ptr(maxQueryLen)
	s.Properties["k"].Minimum, s.Properties["k"].Maximum = ptr(1.0), ptr(float64(maxK))
	s.Properties["max_tokens"].Minimum, s.Properties["max_tokens"].Maximum = ptr(1.0), ptr(float64(render.HardMaxTokens))
	return s
}

func fetchSchema() *jsonschema.Schema {
	s := infer[FetchIn]()
	s.Properties["max_tokens"].Minimum, s.Properties["max_tokens"].Maximum = ptr(1.0), ptr(float64(render.HardMaxTokens))
	return s
}

func apiSchema() *jsonschema.Schema {
	s := infer[APIIn]()
	enum(s, "language", languages)
	s.Properties["frc_season"].Pattern = seasonRe.String()
	s.Properties["symbol"].MaxLength = ptr(maxSymbolLen)
	s.Properties["limit"].Minimum, s.Properties["limit"].Maximum = ptr(1.0), ptr(50.0)
	return s
}

func infer[T any]() *jsonschema.Schema {
	s, err := jsonschema.For[T](nil)
	if err != nil {
		panic(err) // programmer error: caught by tests at build time
	}
	return s
}

func enum(s *jsonschema.Schema, prop string, vals []string) { s.Properties[prop].Enum = toAny(vals) }

func toAny(vs []string) []any {
	out := make([]any, len(vs))
	for i, v := range vs {
		out[i] = v
	}
	return out
}

func ptr[T any](v T) *T { return &v }
