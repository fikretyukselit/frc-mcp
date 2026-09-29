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

	"github.com/fikretyukselit/frc-mcp/internal/facts"
	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/mcpserver/surface"
	"github.com/fikretyukselit/frc-mcp/internal/project"
	"github.com/fikretyukselit/frc-mcp/internal/render"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/verify"
)

// Server is the frc-mcp MCP server. The engine can be swapped atomically
// (index sync) while requests are in flight.
type Server struct {
	mcp    *mcp.Server
	engine atomic.Pointer[retrieve.Engine]
	now    func() time.Time
	opt    Options
}

// Options configures the server.
type Options struct {
	Version string
	Now     func() time.Time // for deterministic tests
	// ProjectRoot is the default root for frc_context (normally the server's
	// working directory, which MCP clients set to the workspace).
	ProjectRoot string
	// NoFilesystem disables filesystem-reading arguments (HTTP / hosted mode,
	// docs/security.md §2.3): frc_context accepts only declare.
	NoFilesystem bool
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
	s := &Server{now: opt.Now, opt: opt}
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
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_context", Title: "Detect FRC project versions", Description: surface.Context(),
		Annotations: readOnly("Detect FRC project versions"), InputSchema: contextSchema()}, s.context)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_vendordep", Title: "Resolve FRC vendordeps", Description: surface.Vendordep(),
		Annotations: readOnly("Resolve FRC vendordeps"), InputSchema: vendordepSchema()}, s.vendordep)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_verify_code", Title: "Verify robot code against the season API", Description: surface.VerifyCode(),
		Annotations: readOnly("Verify robot code against the season API"), InputSchema: verifySchema()}, s.verifyCode)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_whats_new", Title: "FRC library release notes", Description: surface.WhatsNew(),
		Annotations: readOnly("FRC library release notes"), InputSchema: whatsNewSchema()}, s.whatsNew)
	mcp.AddTool(s.mcp, &mcp.Tool{Name: "frc_hardware", Title: "FRC motor specs", Description: surface.Hardware(),
		Annotations: readOnly("FRC motor specs"), InputSchema: hardwareSchema()}, s.hardware)
	s.mcp.AddResource(&mcp.Resource{URI: "frc://index/manifest", Name: "index-manifest", Title: "Loaded index shards",
		Description: "Shards currently loaded: names, build ids, build times, seasons, chunk and symbol counts.",
		MIMEType:    "application/json"}, s.manifest)
}

// ---- frc_search ----

// SearchIn is frc_search's input.
type SearchIn struct {
	Query          string   `json:"query" jsonschema:"what you are looking for, in natural language or with exact class/method names"`
	Pin            string   `json:"pin,omitempty" jsonschema:"pin handle from frc_context; supplies season and language unless given explicitly"`
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
	fromPin, err := applyPin(in.Pin, &in.Season, &in.Language, &in.Channel)
	if err != nil {
		return nil, render.SearchOut{}, err
	}
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
	if fromPin && out.PinSource == "arg" {
		out.PinSource = "handle"
	}
	out.IndexStale = e.Stale(s.now())
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
	out.IndexStale = e.Stale(s.now())
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
	Pin      string `json:"pin,omitempty" jsonschema:"pin handle from frc_context; supplies season and language unless given explicitly"`
	Season   string `json:"frc_season,omitempty" jsonschema:"FRC season, e.g. 2026. Defaults to the current stable season"`
	Language string `json:"language,omitempty" jsonschema:"java, cpp or python; empty searches all languages"`
	Limit    int    `json:"limit,omitempty" jsonschema:"maximum matches (default 10, max 50)"`
}

func (s *Server) api(ctx context.Context, _ *mcp.CallToolRequest, in APIIn) (*mcp.CallToolResult, render.APIOut, error) {
	in.Symbol = strings.TrimSpace(in.Symbol)
	var channel string
	fromPin, err := applyPin(in.Pin, &in.Season, &in.Language, &channel)
	if err != nil {
		return nil, render.APIOut{}, err
	}
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
	switch {
	case fromPin:
		out.PinSource = "handle"
	case in.Season != "":
		out.PinSource = "arg"
	}
	out.Language = in.Language
	out.IndexStale = e.Stale(s.now())
	return text(render.APIMarkdown(out)), out, nil
}

// ---- frc_context ----

// Declare is an explicit pin set.
type Declare struct {
	Season   string `json:"frc_season,omitempty" jsonschema:"FRC season, e.g. 2026"`
	Channel  string `json:"channel,omitempty" jsonschema:"stable, beta or alpha"`
	Language string `json:"language,omitempty" jsonschema:"java, cpp or python"`
}

// ContextIn is frc_context's input.
type ContextIn struct {
	ProjectRoot string   `json:"project_root,omitempty" jsonschema:"robot project directory (default: the server's working directory); unavailable on hosted servers"`
	Declare     *Declare `json:"declare,omitempty" jsonschema:"declare the pin set instead of (or on top of) detecting it"`
}

func (s *Server) context(_ context.Context, _ *mcp.CallToolRequest, in ContextIn) (*mcp.CallToolResult, render.ContextOut, error) {
	out := render.ContextOut{Envelope: render.Envelope{Status: retrieve.StatusOK, Confidence: 1, Freshness: "shard",
		PinSource: "project"}, Vendordeps: []render.DetectedVendordep{}, Files: []string{}}
	pin := project.Pin{}
	if !s.opt.NoFilesystem || in.ProjectRoot != "" {
		if s.opt.NoFilesystem {
			return nil, out, errors.New("project_root is disabled on this (hosted) server; pass declare instead, e.g. {\"declare\": {\"frc_season\": \"2026\", \"language\": \"java\"}}")
		}
		root := in.ProjectRoot
		if root == "" {
			root = s.opt.ProjectRoot
		}
		if root == "" {
			root = "."
		}
		p, err := project.Detect(root)
		switch {
		case errors.Is(err, project.ErrNotProject) && in.Declare != nil:
		case err != nil:
			return nil, out, fmt.Errorf("%w in %s; pass project_root pointing at the robot project, or declare the season: {\"declare\": {\"frc_season\": \"2026\"}}", err, root)
		default:
			pin = p.PinOf()
			out.WPILib, out.Files, out.Warnings = p.WPILib, p.Files, p.Warnings
			for _, v := range p.Vendordeps {
				out.Vendordeps = append(out.Vendordeps, render.DetectedVendordep{File: v.File, Name: v.Name, Version: v.Version, FRCYear: v.FRCYear, UUID: v.UUID})
			}
		}
	}
	if d := in.Declare; d != nil {
		if err := validateCommon(d.Season, d.Channel, d.Language); err != nil {
			return nil, out, err
		}
		if d.Season != "" {
			pin.Season = d.Season
		}
		if d.Channel != "" {
			pin.Channel = d.Channel
		}
		if d.Language != "" {
			pin.Language = d.Language
		}
		out.PinSource = "arg"
	}
	if pin.Season == "" {
		return nil, out, errors.New("could not determine the FRC season; pass declare.frc_season")
	}
	out.Season, out.Channel, out.Language, out.Libraries = pin.Season, pin.Channel, pin.Language, pin.Libs
	out.Pin = pin.Encode()
	if e := s.engine.Load(); e.Ready() && !e.HasSeason(pin.Season) {
		out.Status = retrieve.StatusVersionMismatch
		out.Warnings = append(out.Warnings, fmt.Sprintf("the loaded index has no data for season %s", pin.Season))
	}
	if e := s.engine.Load(); e.Ready() && !e.Catalog().Empty() && len(out.Vendordeps) > 0 {
		var installed []facts.Installed
		for _, v := range out.Vendordeps {
			installed = append(installed, facts.Installed{Name: v.Name, Version: v.Version, UUID: v.UUID, FRCYear: v.FRCYear})
		}
		for _, f := range e.Catalog().Check(installed, pin.Season) {
			if f.Status == "outdated" || f.Status == "wrong_year" {
				out.Upgrades = append(out.Upgrades, render.UpgradeHint{Name: f.Name, Installed: f.Installed, Latest: f.Latest, Status: f.Status, Fix: f.Fix})
			}
		}
	}
	out.Next = []string{fmt.Sprintf("pass {\"pin\": %q} to frc_search, frc_api and frc_vendordep", out.Pin)}
	return text(render.ContextMarkdown(out)), out, nil
}

// ---- frc_verify_code ----

const maxVerifyBytes = 512 << 10

// VerifyIn is frc_verify_code's input.
type VerifyIn struct {
	Code     string `json:"code,omitempty" jsonschema:"Java source to check"`
	Path     string `json:"path,omitempty" jsonschema:"alternative to code: a .java file under the project root (local servers only)"`
	Language string `json:"language,omitempty" jsonschema:"java (C++ and Python arrive in M3)"`
	Season   string `json:"frc_season,omitempty" jsonschema:"FRC season to check against (default: pin, then the current stable season)"`
	Pin      string `json:"pin,omitempty" jsonschema:"pin handle from frc_context"`
}

func (s *Server) verifyCode(ctx context.Context, _ *mcp.CallToolRequest, in VerifyIn) (*mcp.CallToolResult, render.VerifyOut, error) {
	var lang, channel string
	fromPin, err := applyPin(in.Pin, &in.Season, &lang, &channel)
	if err != nil {
		return nil, render.VerifyOut{}, err
	}
	if in.Language == "" {
		in.Language = "java"
	}
	if err := validateCommon(in.Season, "", in.Language); err != nil {
		return nil, render.VerifyOut{}, err
	}
	code, file := in.Code, ""
	switch {
	case in.Code != "" && in.Path != "":
		return nil, render.VerifyOut{}, errors.New("pass either code or path, not both")
	case in.Path != "":
		if s.opt.NoFilesystem {
			return nil, render.VerifyOut{}, errors.New("path is disabled on this (hosted) server; pass the file content as code")
		}
		root := s.opt.ProjectRoot
		if root == "" {
			root = "."
		}
		b, err := project.ReadSource(root, in.Path, maxVerifyBytes)
		if err != nil {
			return nil, render.VerifyOut{}, fmt.Errorf("%w; path must be a .java file under the project root %s", err, root)
		}
		code, file = string(b), in.Path
	case in.Code == "":
		return nil, render.VerifyOut{}, errors.New(`pass code (Java source) or path, e.g. {"code": "import edu.wpi.first.wpilibj.TimedRobot; …"}`)
	case len(in.Code) > maxVerifyBytes:
		return nil, render.VerifyOut{}, fmt.Errorf("code is %d bytes; the limit is %d — verify one file per call", len(in.Code), maxVerifyBytes)
	}
	e := s.engine.Load()
	if !e.Ready() {
		out := render.VerifyOut{Envelope: syncing(), Findings: []render.VerifyFinding{}}
		return text(render.VerifyMarkdown(out)), out, nil
	}
	pinSource := "arg"
	switch {
	case fromPin:
		pinSource = "handle"
	case in.Season == "":
		in.Season, pinSource = e.DefaultSeason(), "default"
	}
	out := render.VerifyOut{Envelope: render.Envelope{Status: retrieve.StatusOK, Confidence: 1, Freshness: "shard",
		Season: in.Season, PinSource: pinSource, Language: in.Language}, File: file, Findings: []render.VerifyFinding{}}
	if in.Language != "java" {
		out.Status, out.Confidence = retrieve.StatusNoMatch, 0
		out.Coverage = map[string]string{"wpilib": "none (" + in.Language + " verification arrives in M4; its symbols are searchable with frc_api)"}
		out.Next = []string{"only Java is verified today; use frc_api to check individual " + in.Language + " symbols"}
		return text(render.VerifyMarkdown(out)), out, nil
	}
	// A file under the project root: use the project's vendordep versions so
	// version skew within a season caps vendor findings (verify.Options).
	opt := verify.Options{}
	if file != "" {
		root := s.opt.ProjectRoot
		if root == "" {
			root = "."
		}
		if p, err := project.Detect(root); err == nil {
			opt.Installed = map[string]string{}
			for _, v := range p.Vendordeps {
				if lib := verify.LibraryOfVendordep(v.Name); lib != "" {
					opt.Installed[lib] = v.Version
				}
			}
		}
	}
	r := verify.JavaWith(ctx, e, code, in.Season, opt)
	out.Coverage, out.Checked, out.Errors, out.Warnings = r.Coverage, r.Checked, r.Errors, r.Warnings
	for _, f := range r.Findings {
		out.Findings = append(out.Findings, render.VerifyFinding(f))
	}
	if r.Errors > 0 {
		out.Status = retrieve.StatusVersionMismatch
		out.Next = []string{"apply the fixes above, then run frc_verify_code again"}
	}
	return text(render.VerifyMarkdown(out)), out, nil
}

// ---- frc_whats_new ----

// WhatsNewIn is frc_whats_new's input.
type WhatsNewIn struct {
	Library    string `json:"library,omitempty" jsonschema:"library id or alias (wpilib, phoenix6, revlib, photonvision, pathplannerlib, choreolib, advantagekit, yagsl) or all (default)"`
	Since      string `json:"since,omitempty" jsonschema:"only releases after this: a version of the library (e.g. 2026.0.2) or a date (YYYY-MM-DD)"`
	Season     string `json:"frc_season,omitempty" jsonschema:"FRC season, e.g. 2026 (default: pin, else every season)"`
	StableOnly bool   `json:"stable_only,omitempty" jsonschema:"leave out alpha and beta releases"`
	Limit      int    `json:"limit,omitempty" jsonschema:"maximum entries (default 10)"`
	Pin        string `json:"pin,omitempty" jsonschema:"pin handle from frc_context"`
}

func (s *Server) whatsNew(_ context.Context, _ *mcp.CallToolRequest, in WhatsNewIn) (*mcp.CallToolResult, render.WhatsNewOut, error) {
	var lang, channel string
	fromPin, err := applyPin(in.Pin, &in.Season, &lang, &channel)
	if err != nil {
		return nil, render.WhatsNewOut{}, err
	}
	if err := validateCommon(in.Season, "", ""); err != nil {
		return nil, render.WhatsNewOut{}, err
	}
	if in.Limit <= 0 {
		in.Limit = 10
	}
	e := s.engine.Load()
	if !e.Ready() {
		out := render.WhatsNewOut{Envelope: syncing(), Entries: []render.ReleaseEntry{}}
		return text(render.WhatsNewMarkdown(out)), out, nil
	}
	q := retrieve.WhatsNewQuery{Library: in.Library, Season: in.Season, Stable: in.StableOnly, Limit: in.Limit}
	since := strings.TrimSpace(in.Since)
	if t, err := time.Parse("2006-01-02", since); err == nil {
		q.After = t
	} else if since != "" {
		if retrieve.LibraryID(in.Library) == "" {
			return nil, render.WhatsNewOut{}, errors.New(`a version in "since" needs a library, e.g. {"library": "revlib", "since": "2026.0.2"}; use a date (YYYY-MM-DD) for all libraries`)
		}
		q.Since = since
	}
	pinSource := "arg"
	switch {
	case fromPin:
		pinSource = "handle"
	case in.Season == "":
		pinSource = "all seasons"
	}
	out := render.WhatsNewOut{Envelope: render.Envelope{Status: retrieve.StatusOK, Confidence: 1, Freshness: "shard",
		Season: in.Season, PinSource: pinSource}, Entries: []render.ReleaseEntry{}}
	for _, r := range e.WhatsNew(q) {
		out.Entries = append(out.Entries, render.ReleaseEntry{Library: r.Library, Version: r.Version, Season: r.Season,
			Channel: r.Channel, Published: r.PublishedAt.Format("2006-01-02"), Breaking: r.Breaking, Title: r.Title,
			Summary: r.Summary, ChunkID: r.ChunkID,
			Citation: render.Citation{SourceURL: r.SourceURL, Library: r.Library, Version: r.Version, UpstreamRev: r.UpstreamRev,
				RetrievedAt: r.RetrievedAt.Format(time.RFC3339), License: r.License, Trust: r.Trust}})
	}
	if len(out.Entries) == 0 {
		out.Status, out.Confidence = retrieve.StatusNoMatch, 0
		out.Libraries = e.ReleaseLibraries()
		out.Next = []string{"no releases match; widen since/frc_season, or pick a library listed above"}
	}
	return text(render.WhatsNewMarkdown(out)), out, nil
}

// ---- frc_hardware ----

// HardwareIn is frc_hardware's input.
type HardwareIn struct {
	Parts    []string `json:"parts,omitempty" jsonschema:"part names or aliases, e.g. kraken x60, kraken x60 foc, neo vortex, falcon, neo550"`
	Category string   `json:"category,omitempty" jsonschema:"list every part of a category instead (motor)"`
	Season   string   `json:"frc_season,omitempty" jsonschema:"FRC season (default: pin, then the current stable season)"`
	Pin      string   `json:"pin,omitempty" jsonschema:"pin handle from frc_context"`
}

func (s *Server) hardware(ctx context.Context, _ *mcp.CallToolRequest, in HardwareIn) (*mcp.CallToolResult, render.HardwareOut, error) {
	var lang, channel string
	fromPin, err := applyPin(in.Pin, &in.Season, &lang, &channel)
	if err != nil {
		return nil, render.HardwareOut{}, err
	}
	if err := validateCommon(in.Season, "", ""); err != nil {
		return nil, render.HardwareOut{}, err
	}
	if len(in.Parts) == 0 && in.Category == "" {
		return nil, render.HardwareOut{}, errors.New(`pass parts (e.g. {"parts": ["kraken x60", "neo vortex"]}) or a category (e.g. {"category": "motor"})`)
	}
	e := s.engine.Load()
	if !e.Ready() {
		out := render.HardwareOut{Envelope: syncing(), Parts: []render.HWPartOut{}}
		return text(render.HardwareMarkdown(out)), out, nil
	}
	pinSource := "arg"
	switch {
	case fromPin:
		pinSource = "handle"
	case in.Season == "":
		in.Season, pinSource = e.DefaultSeason(), "default"
	}
	out := render.HardwareOut{Envelope: render.Envelope{Status: retrieve.StatusOK, Confidence: 1, Freshness: "shard",
		Season: in.Season, PinSource: pinSource}, Parts: []render.HWPartOut{}}
	parts, unknown := e.Hardware(in.Parts, in.Category, in.Season)
	for _, p := range parts {
		po := render.HWPartOut{Part: p.Part, Name: p.Name, Category: p.Category}
		for _, r := range p.Rows {
			po.Sources = append(po.Sources, render.HWSourceRow{Source: r.Source, Season: r.Season, Fields: r.Fields,
				Factory: r.Factory, Note: r.Note, Citation: render.Citation{SourceURL: r.SourceURL, Library: "wpilib",
					Version: r.UpstreamRev, UpstreamRev: r.UpstreamRev, RetrievedAt: r.RetrievedAt.Format(time.RFC3339),
					License: r.License, Trust: r.Trust}})
			if r.Source == "wpilib-dcmotor" && po.Sim == nil {
				po.Sim = map[string]string{}
				for _, l := range []string{"java", "cpp", "python"} {
					if f := e.SimFactory(ctx, r.Factory, r.Season, l); f != "" {
						po.Sim[l] = callForm(f, l)
					}
				}
			}
		}
		out.Parts = append(out.Parts, po)
	}
	out.Unknown = unknown
	if len(out.Parts) == 0 {
		out.Status, out.Confidence = retrieve.StatusNoMatch, 0
		out.Known = e.HardwareParts()
		out.Next = []string{"use one of the known part ids listed above"}
	} else if len(unknown) > 0 {
		out.Known = e.HardwareParts()
	}
	return text(render.HardwareMarkdown(out)), out, nil
}

// callForm renders a factory FQN as a call: "frc::DCMotor#NEO" →
// "frc::DCMotor::NEO(numMotors)", "pkg.DCMotor#getNEO" → "pkg.DCMotor.getNEO(numMotors)".
func callForm(fqn, lang string) string {
	owner, member, _ := strings.Cut(fqn, "#")
	sep := "."
	if lang == "cpp" {
		sep = "::"
	}
	return owner + sep + member + "(numMotors)"
}

// ---- frc_vendordep ----

// VendordepIn is frc_vendordep's input.
type VendordepIn struct {
	Name       string   `json:"name,omitempty" jsonschema:"library name or alias (rev, phoenix6, photon, pathplanner, choreo, advantagekit, yagsl, …)"`
	Vendordeps []string `json:"vendordeps,omitempty" jsonschema:"set mode: installed vendordeps as raw JSON documents or name@version"`
	Season     string   `json:"frc_season,omitempty" jsonschema:"FRC season, e.g. 2026 (default: pin, then the current stable season)"`
	Pin        string   `json:"pin,omitempty" jsonschema:"pin handle from frc_context; set mode checks the project's own vendordeps"`
}

func (s *Server) vendordep(_ context.Context, _ *mcp.CallToolRequest, in VendordepIn) (*mcp.CallToolResult, render.VendordepOut, error) {
	var lang, channel string
	fromPin, err := applyPin(in.Pin, &in.Season, &lang, &channel)
	if err != nil {
		return nil, render.VendordepOut{}, err
	}
	if err := validateCommon(in.Season, "", ""); err != nil {
		return nil, render.VendordepOut{}, err
	}
	e := s.engine.Load()
	if !e.Ready() || e.Catalog().Empty() {
		out := render.VendordepOut{Envelope: syncing()}
		return text(render.VendordepMarkdown(out)), out, nil
	}
	pinSource := "arg"
	switch {
	case fromPin:
		pinSource = "handle"
	case in.Season == "":
		in.Season, pinSource = e.DefaultSeason(), "default"
	}
	out := render.VendordepOut{Envelope: render.Envelope{Status: retrieve.StatusOK, Confidence: 1, Freshness: "shard",
		Season: in.Season, PinSource: pinSource}}
	cat := e.Catalog()

	// Set mode: explicit list, or the pin's libraries.
	var installed []facts.Installed
	for _, raw := range in.Vendordeps {
		v, err := facts.ParseInstalled(raw)
		if err != nil {
			return nil, out, err
		}
		installed = append(installed, v)
	}
	if len(installed) == 0 && in.Name == "" && in.Pin != "" {
		p, _ := project.DecodePin(in.Pin)
		for name, ver := range p.Libs {
			if name != "wpilib" {
				installed = append(installed, facts.Installed{Name: name, Version: ver})
			}
		}
		slices.SortFunc(installed, func(a, b facts.Installed) int { return strings.Compare(a.Name, b.Name) })
	}
	if len(installed) > 0 {
		for _, f := range cat.Check(installed, in.Season) {
			out.Findings = append(out.Findings, render.CompatFinding(f))
			if f.Status == "wrong_year" || f.Status == "conflict" {
				out.Status = retrieve.StatusVersionMismatch
			}
		}
		return text(render.VendordepMarkdown(out)), out, nil
	}
	if in.Name == "" {
		return nil, out, errors.New(`pass name (e.g. {"name": "rev"}) or vendordeps (e.g. {"vendordeps": ["REVLib@2026.0.0"]}) or a frc_context pin`)
	}
	m, cands := cat.Resolve(in.Name, in.Season)
	if m == nil {
		out.Status, out.Confidence = retrieve.StatusNoMatch, 0
		out.Candidates = cands
		if len(cands) == 0 {
			out.Candidates = cat.Names(in.Season)
		}
		out.Next = []string{"retry with one of the catalog library names listed above"}
		return text(render.VendordepMarkdown(out)), out, nil
	}
	l := m.Latest
	info := &render.VendordepInfo{Name: l.Name, UUID: l.UUID, Latest: l.Version, Versions: m.Versions, FRCYear: l.FRCYear,
		FileName: l.FileName, JSONURL: l.JSONURL, MavenURLs: l.MavenURLs,
		Citation: render.Citation{SourceURL: l.SourceURL, Library: "vendordeps", Version: l.Version, UpstreamRev: l.UpstreamRev,
			RetrievedAt: l.RetrievedAt.Format(time.RFC3339), License: "vendor-specific", Trust: "official"}}
	if l.JSONURL != "" {
		info.Install = "./gradlew vendordep --url=" + l.JSONURL
	}
	for _, c := range l.Conflicts {
		info.ConflictsWith = append(info.ConflictsWith, c.ErrorMessage)
	}
	if yr := l.FRCYear; yr != "" && yr != in.Season {
		out.Status = retrieve.StatusVersionMismatch
		out.Next = append(out.Next, fmt.Sprintf("the newest catalog entry declares frcYear %s, not %s: the vendor may not have published a %s release yet", yr, in.Season, in.Season))
	}
	out.Library = info
	return text(render.VendordepMarkdown(out)), out, nil
}

// applyPin fills unset fields from a pin handle; it reports whether the
// handle supplied the season.
func applyPin(h string, season, language, channel *string) (bool, error) {
	if h == "" {
		return false, nil
	}
	p, err := project.DecodePin(h)
	if err != nil {
		return false, fmt.Errorf("%w; call frc_context to get a fresh pin", err)
	}
	fromPin := *season == "" && p.Season != ""
	if *season == "" {
		*season = p.Season
	}
	if *language == "" {
		*language = p.Language
	}
	if *channel == "" {
		*channel = p.Channel
	}
	return fromPin, nil
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

func contextSchema() *jsonschema.Schema {
	s := infer[ContextIn]()
	d := s.Properties["declare"]
	enum(d, "language", []string{"java", "cpp", "python"})
	enum(d, "channel", channels)
	d.Properties["frc_season"].Pattern = seasonRe.String()
	return s
}

func verifySchema() *jsonschema.Schema {
	s := infer[VerifyIn]()
	enum(s, "language", []string{"java", "cpp", "python"})
	s.Properties["frc_season"].Pattern = seasonRe.String()
	s.Properties["code"].MaxLength = ptr(maxVerifyBytes)
	return s
}

func vendordepSchema() *jsonschema.Schema {
	s := infer[VendordepIn]()
	s.Properties["frc_season"].Pattern = seasonRe.String()
	s.Properties["vendordeps"].MaxItems = ptr(64)
	return s
}

func hardwareSchema() *jsonschema.Schema {
	s := infer[HardwareIn]()
	s.Properties["frc_season"].Pattern = seasonRe.String()
	s.Properties["parts"].MaxItems = ptr(20)
	enum(s, "category", []string{"motor"})
	return s
}

func whatsNewSchema() *jsonschema.Schema {
	s := infer[WhatsNewIn]()
	s.Properties["frc_season"].Pattern = seasonRe.String()
	s.Properties["since"].MaxLength = ptr(40)
	s.Properties["library"].MaxLength = ptr(40)
	s.Properties["limit"].Minimum, s.Properties["limit"].Maximum = ptr(1.0), ptr(50.0)
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
