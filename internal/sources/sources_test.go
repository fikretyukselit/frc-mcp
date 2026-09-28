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
		"http url":    func(s *Source) { s.URL = "http://a/b" },
		"bad adapter": func(s *Source) { s.Adapter = "scrape-anything" },
		"no license":  func(s *Source) { s.License = "" },
	} {
		s := base
		mut(&s)
		if err := (&Registry{Version: 1, Sources: []Source{s}}).Validate(); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}
