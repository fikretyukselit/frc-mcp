// Package sources loads and validates data/sources.yaml, the declarative
// registry of everything frc-mcp ingests (docs/sources.md).
package sources

import (
	"fmt"
	"net/url"
	"os"
	"slices"
	"strings"

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
}

// Model is an embedding model distributed alongside shards.
type Model struct {
	ID      string            `yaml:"id"`
	License string            `yaml:"license"`
	Files   map[string]string `yaml:"files"`
}

// Adapters known to this binary.
var Adapters = []string{"sphinx-htmlzip", "javadoc-zip", "vendordep-catalog", "github-markdown", "gitbook-llms", "github-releases"}

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
