// Package sources loads and validates data/sources.yaml, the declarative
// registry of everything frc-mcp ingests (docs/sources.md).
package sources

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"
)

// Registry is the parsed sources.yaml.
type Registry struct {
	Version    int      `yaml:"version"`
	UserAgent  string   `yaml:"user_agent"`
	ExtraHosts []string `yaml:"extra_hosts"`
	Sources    []Source `yaml:"sources"`
	Models     []Model  `yaml:"models"`
}

// Source is one ingested upstream.
type Source struct {
	ID       string `yaml:"id"`
	Adapter  string `yaml:"adapter"`
	URL      string `yaml:"url"`
	ZipRoot  string `yaml:"zip_root"`
	BaseURL  string `yaml:"base_url"`
	Library  string `yaml:"library"`
	Season   string `yaml:"season"`
	Channel  string `yaml:"channel"`
	Version  string `yaml:"version"`
	Language string `yaml:"language"`
	License  string `yaml:"license"`
	Trust    string `yaml:"trust"`
	Shard    string `yaml:"shard"`
	Verified bool   `yaml:"verified"`

	// Markdown adapters (github-markdown, gitbook-llms).
	Include   string   `yaml:"include"`   // path prefix inside the archive / site to ingest
	Skip      []string `yaml:"skip"`      // path prefixes (relative to include) to leave out
	URLStyle  string   `yaml:"url_style"` // html (a/b.html) | dir (a/b/) | plain (a/b); github-markdown only
	Lowercase bool     `yaml:"lowercase"` // lower-case page URLs (Writerside)

	// Forum feeds (discourse-rss). URL is the topics feed (latest.rss);
	// PostsURL, when set, is the latest-posts feed (posts.rss), whose replies
	// are kept only for topics that URL's feed lists in an allowed category.
	PostsURL   string   `yaml:"posts_url"`
	Categories []string `yaml:"categories"` // category names to keep (case-insensitive); empty = all

	// MinInterval is the politeness floor between two fetches of this
	// source (Go duration, e.g. "30m"). Within it the cached copy is served
	// without a request; it matters for hosts that send no validators.
	MinInterval string `yaml:"min_interval"`

	// Hardware labels the hw_spec rows of a vendor spec-page adapter
	// (gitbook-spec-table): one page describes one part.
	Hardware *Hardware `yaml:"hardware"`
}

// Hardware names what a spec page describes and how its rows are labeled.
type Hardware struct {
	Source string `yaml:"source"` // hw_spec source label, e.g. "rev-docs"; never shared with another upstream
	Part   string `yaml:"part"`   // part id, the same one other sources use (e.g. "neovortex")
	Name   string `yaml:"name"`   // display name, e.g. "NEO Vortex"
}

// Interval parses MinInterval (0 when unset; Validate rejects bad values).
func (s Source) Interval() time.Duration {
	d, _ := time.ParseDuration(s.MinInterval)
	return d
}

// Model is an embedding model distributed alongside shards.
type Model struct {
	ID      string            `yaml:"id"`
	License string            `yaml:"license"`
	Files   map[string]string `yaml:"files"`
}

// Adapters known to this binary.
var Adapters = []string{"sphinx-htmlzip", "javadoc-zip", "vendordep-catalog", "github-markdown", "gitbook-llms", "github-releases", "pypi-wheel", "doxygen-zip", "wpilib-dcmotor", "recalc-motors", "gitbook-spec-table", "discourse-rss"}

// partRe matches a hw_spec part id or source label ("krakenx60-foc",
// "andymarkrs775_125", "rev-docs").
var partRe = regexp.MustCompile(`^[a-z0-9]+(?:[-_][a-z0-9]+)*$`)

// Load parses and validates a registry file.
func Load(path string) (*Registry, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r Registry
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil {
		return nil, fmt.Errorf("sources: %s: %w", path, err)
	}
	return &r, r.Validate()
}

// Validate checks every entry; unknown adapters, non-https URLs and missing
// provenance fields are errors.
func (r *Registry) Validate() error {
	if r.Version != 1 {
		return fmt.Errorf("sources: unsupported version %d", r.Version)
	}
	seen := map[string]bool{}
	for _, s := range r.Sources {
		if s.ID == "" || seen[s.ID] {
			return fmt.Errorf("sources: missing or duplicate id %q", s.ID)
		}
		seen[s.ID] = true
		if !slices.Contains(Adapters, s.Adapter) {
			return fmt.Errorf("sources: %s: unknown adapter %q (known: %v)", s.ID, s.Adapter, Adapters)
		}
		for name, v := range map[string]string{"url": s.URL, "base_url": s.BaseURL} {
			u, err := url.Parse(v)
			if err != nil || u.Scheme != "https" || u.Host == "" {
				return fmt.Errorf("sources: %s: %s must be an https URL", s.ID, name)
			}
		}
		if s.Adapter == "github-markdown" && !slices.Contains([]string{"html", "dir", "plain"}, s.URLStyle) {
			return fmt.Errorf("sources: %s: url_style must be html, dir or plain", s.ID)
		}
		if s.MinInterval != "" {
			if d, err := time.ParseDuration(s.MinInterval); err != nil || d <= 0 {
				return fmt.Errorf("sources: %s: min_interval must be a positive Go duration (e.g. 30m)", s.ID)
			}
		}
		if s.Adapter == "discourse-rss" {
			if err := validateForum(s); err != nil {
				return fmt.Errorf("sources: %s: %w", s.ID, err)
			}
		} else if s.PostsURL != "" || len(s.Categories) > 0 {
			return fmt.Errorf("sources: %s: posts_url and categories are discourse-rss options", s.ID)
		}
		if s.Adapter == "gitbook-spec-table" && (s.Hardware == nil || !partRe.MatchString(s.Hardware.Part) ||
			!partRe.MatchString(s.Hardware.Source) || strings.TrimSpace(s.Hardware.Name) == "") {
			return fmt.Errorf("sources: %s: gitbook-spec-table needs hardware.source, hardware.part (lower-case id) and hardware.name", s.ID)
		}
		for name, v := range map[string]string{"library": s.Library, "season": s.Season, "channel": s.Channel,
			"version": s.Version, "license": s.License, "trust": s.Trust, "shard": s.Shard} {
			if strings.TrimSpace(v) == "" {
				return fmt.Errorf("sources: %s: missing %s", s.ID, name)
			}
		}
	}
	for _, m := range r.Models {
		for f, v := range m.Files {
			if u, err := url.Parse(v); err != nil || u.Scheme != "https" {
				return fmt.Errorf("sources: model %s: %s must be https", m.ID, f)
			}
		}
	}
	return nil
}

// validateForum enforces the forum policy in configuration, so a registry
// edit cannot quietly change it (docs/security.md §2.1, docs/sources.md §5):
//
//   - forum posts are user content: trust must be community and the license a
//     LicenseRef-*-UserContent id, which `index publish` never ships;
//   - site-level feeds only: Discourse's default robots.txt disallows the
//     per-category and per-topic feeds (/c/*.rss, /t/*/*.rss).
func validateForum(s Source) error {
	if s.Trust != "community" {
		return fmt.Errorf("discourse-rss content must be trust: community (got %q)", s.Trust)
	}
	if !UserContent(s.License) {
		return fmt.Errorf("discourse-rss license must be a LicenseRef-*-UserContent id (never redistributed), got %q", s.License)
	}
	feeds := []string{s.URL}
	if s.PostsURL != "" {
		feeds = append(feeds, s.PostsURL)
	}
	var host string
	for i, raw := range feeds {
		u, err := url.Parse(raw)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("feed %q must be an https URL", raw)
		}
		if !strings.HasSuffix(u.Path, ".rss") || strings.HasPrefix(u.Path, "/c/") || strings.HasPrefix(u.Path, "/t/") ||
			u.RawQuery != "" {
			return fmt.Errorf("feed %q: only site-level .rss feeds are allowed (robots.txt disallows /c/*.rss and /t/*/*.rss)", raw)
		}
		if i == 0 {
			host = u.Host
		} else if u.Host != host {
			return fmt.Errorf("posts_url must be on the same host as url")
		}
	}
	return nil
}

// UserContent reports whether a license id marks user-contributed content
// (forum posts) that is never redistributed, even with --include-unlicensed.
func UserContent(license string) bool {
	return strings.HasPrefix(license, "LicenseRef-") && strings.HasSuffix(license, "-UserContent")
}

// Hosts returns the egress allowlist derived from the registry.
func (r *Registry) Hosts() []string {
	var hs []string
	add := func(raw string) {
		if u, err := url.Parse(raw); err == nil && !slices.Contains(hs, u.Hostname()) {
			hs = append(hs, u.Hostname())
		}
	}
	for _, s := range r.Sources {
		add(s.URL)
		if s.PostsURL != "" {
			add(s.PostsURL)
		}
	}
	for _, m := range r.Models {
		for _, f := range m.Files {
			add(f)
		}
	}
	for _, h := range r.ExtraHosts {
		if !slices.Contains(hs, h) {
			hs = append(hs, h)
		}
	}
	return hs
}

// UA renders the user agent for a binary version.
func (r *Registry) UA(version string) string {
	return strings.ReplaceAll(r.UserAgent, "{version}", version)
}

// Select returns sources whose id matches any of ids (all when empty).
func (r *Registry) Select(ids ...string) []Source {
	if len(ids) == 0 {
		return r.Sources
	}
	var out []Source
	for _, s := range r.Sources {
		if slices.Contains(ids, s.ID) {
			out = append(out, s)
		}
	}
	return out
}
