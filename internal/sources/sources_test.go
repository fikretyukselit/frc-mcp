package sources

import (
	"slices"
	"testing"
)

func TestRegistryLoads(t *testing.T) {
	r, err := Load("../../data/sources.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Sources) < 4 {
		t.Fatalf("sources = %d", len(r.Sources))
	}
	hosts := r.Hosts()
	for _, h := range []string{"docs.wpilib.org", "frcmaven.wpi.edu", "storage.googleapis.com", "huggingface.co"} {
		if !slices.Contains(hosts, h) {
			t.Errorf("allowlist missing %s: %v", h, hosts)
		}
	}
	if slices.Contains(hosts, "github.wpilib.org") {
		t.Error("github.wpilib.org is link-only (robots.txt disallows /docs/beta) and must not be on the fetch allowlist")
	}
}

func TestValidateRejects(t *testing.T) {
	base := Source{ID: "x", Adapter: "javadoc-zip", URL: "https://a/b", BaseURL: "https://a/", Library: "l",
		Season: "2026", Channel: "stable", Version: "1", License: "MIT", Trust: "official", Shard: "s"}
	for name, mut := range map[string]func(*Source){
		"http url":                      func(s *Source) { s.URL = "http://a/b" },
		"bad adapter":                   func(s *Source) { s.Adapter = "scrape-anything" },
		"no license":                    func(s *Source) { s.License = "" },
		"bad min_interval":              func(s *Source) { s.MinInterval = "soon" },
		"negative interval":             func(s *Source) { s.MinInterval = "-5m" },
		"forum options on docs adapter": func(s *Source) { s.Categories = []string{"Programming"} },
	} {
		s := base
		mut(&s)
		if err := (&Registry{Version: 1, Sources: []Source{s}}).Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

// The forum policy lives in validation so no registry edit can turn forum
// posts into redistributable or trusted content, or point at feeds that
// robots.txt disallows.
func TestValidateForumPolicy(t *testing.T) {
	base := Source{ID: "cd", Adapter: "discourse-rss", URL: "https://forum.example/latest.rss",
		PostsURL: "https://forum.example/posts.rss", BaseURL: "https://forum.example/", Library: "chiefdelphi",
		Season: "rolling", Channel: "stable", Version: "rolling", License: "LicenseRef-ChiefDelphi-UserContent",
		Trust: "community", Shard: "forum", MinInterval: "30m"}
	if err := (&Registry{Version: 1, Sources: []Source{base}}).Validate(); err != nil {
		t.Fatalf("valid forum source rejected: %v", err)
	}
	for name, mut := range map[string]func(*Source){
		"vendor trust":          func(s *Source) { s.Trust = "vendor" },
		"open license":          func(s *Source) { s.License = "CC-BY-NC-SA-3.0" },
		"plain LicenseRef":      func(s *Source) { s.License = "LicenseRef-ChiefDelphi" },
		"category feed":         func(s *Source) { s.URL = "https://forum.example/c/technical/programming/30.rss" },
		"topic feed":            func(s *Source) { s.PostsURL = "https://forum.example/t/some-topic/123.rss" },
		"json api":              func(s *Source) { s.URL = "https://forum.example/latest.json" },
		"query string":          func(s *Source) { s.URL = "https://forum.example/latest.rss?api_key=x" },
		"posts on another host": func(s *Source) { s.PostsURL = "https://other.example/posts.rss" },
	} {
		s := base
		mut(&s)
		if err := (&Registry{Version: 1, Sources: []Source{s}}).Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if !slices.Contains((&Registry{Version: 1, Sources: []Source{base}}).Hosts(), "forum.example") {
		t.Error("forum host missing from the allowlist")
	}
	if got := base.Interval().String(); got != "30m0s" {
		t.Errorf("interval = %s", got)
	}
}
