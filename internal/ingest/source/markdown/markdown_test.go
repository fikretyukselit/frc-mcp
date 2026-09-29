package markdown

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

var testSrc = sources.Source{Library: "photonvision", Season: "2026", Channel: "stable", Version: "v2026.3.4",
	License: "CC-BY-4.0", Trust: "vendor"}

func chunksOf(t *testing.T, file string) []index.Chunk {
	t.Helper()
	b, err := os.ReadFile("testdata/" + file)
	if err != nil {
		t.Fatal(err)
	}
	cs := Chunks(Page{Rel: strings.TrimSuffix(file, ".md"), URL: "https://example.org/" + file, Text: string(b)},
		testSrc, "rev", time.Unix(0, 0))
	if len(cs) == 0 {
		t.Fatalf("%s: no chunks", file)
	}
	for _, c := range cs {
		if err := c.Validate(); err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		if strings.Count(c.Body, "```")%2 != 0 {
			t.Fatalf("%s: unbalanced fence in\n%s", file, c.Body)
		}
		for _, junk := range []string{"{%", ":::", "{eval-rst}", "<Tab", "<tab", "{style=", "import Tabs", "!!!", "===", ".. code-block", "{ref}", "llms.txt"} {
			if strings.Contains(c.Body, junk) {
				t.Errorf("%s: dialect syntax %q leaked into\n%s", file, junk, c.Body)
			}
		}
	}
	return cs
}

func byLang(cs []index.Chunk, heading, lang string) *index.Chunk {
	for i := range cs {
		if strings.HasSuffix(cs[i].HeadingPath, heading) && cs[i].Language == lang {
			return &cs[i]
		}
	}
	return nil
}

func TestMyST(t *testing.T) {
	cs := chunksOf(t, "myst.md")
	java := byLang(cs, "Enabling MultiTag", "java")
	py := byLang(cs, "Enabling MultiTag", "python")
	if java == nil || py == nil || byLang(cs, "Enabling MultiTag", "cpp") == nil {
		t.Fatalf("want java/cpp/python variants: %+v", cs)
	}
	if !strings.Contains(java.Body, "```java\nvar results = camera.getAllUnreadResults();") || strings.Contains(java.Body, "GetAllUnreadResults") {
		t.Errorf("java variant:\n%s", java.Body)
	}
	if !strings.Contains(java.Body, "`the PhotonPoseEstimator class`") {
		t.Errorf("role not rewritten:\n%s", java.Body)
	}
	if strings.Contains(java.Body, "multitag-ui.png") {
		t.Errorf("image directive kept:\n%s", java.Body)
	}
	intro := byLang(cs, "MultiTag Localization", "any")
	if intro == nil || !strings.Contains(intro.Body, "**Warning:**\nMultiTag requires") {
		t.Errorf("admonition: %+v", intro)
	}
	if java.HeadingPath != "MultiTag Localization › Enabling MultiTag" || java.Anchor != "enabling-multitag" || java.Title != "MultiTag Localization" {
		t.Errorf("path/anchor/title: %q %q %q", java.HeadingPath, java.Anchor, java.Title)
	}
	if java.DocID != "photonvision-docs/2026/myst" || java.SourceURL != "https://example.org/myst.md" {
		t.Errorf("provenance: %s %s", java.DocID, java.SourceURL)
	}
}

func TestDocusaurus(t *testing.T) {
	cs := chunksOf(t, "docusaurus.md")
	s := byLang(cs, "Structured", "java")
	if s == nil || s.Anchor != "structured" || !strings.Contains(s.Body, "**Danger:**") || !strings.Contains(s.Body, "```java\nLogger.recordOutput") {
		t.Fatalf("structured: %+v", s)
	}
	if !strings.Contains(s.Body, "**Windows:**") {
		t.Errorf("non-code tab label lost:\n%s", s.Body)
	}
	if cs[0].Anchor != "supported-types" || cs[0].Title != "📊 Supported Types" {
		t.Errorf("explicit anchor/title: %q %q", cs[0].Anchor, cs[0].Title)
	}
}

func TestWriterside(t *testing.T) {
	cs := chunksOf(t, "writerside.md")
	j, c := byLang(cs, "Using AutoBuilder", "java"), byLang(cs, "Using AutoBuilder", "cpp")
	if j == nil || c == nil || strings.Contains(j.Body, "AutoBuilder::") || !strings.Contains(c.Body, "```cpp\nauto path") {
		t.Fatalf("variants: %+v %+v", j, c)
	}
}

func TestMkDocs(t *testing.T) {
	cs := chunksOf(t, "mkdocs.md")
	j := byLang(cs, "Setting up the Drive Subsystem", "java")
	if j == nil || !strings.Contains(j.Body, "```java\npublic class Drive extends SubsystemBase {\n    private final") {
		t.Fatalf("java: %+v", j)
	}
	if !strings.Contains(j.Body, "**Note:**") || !strings.Contains(j.Body, "**Swerve:**") || strings.Contains(j.Body, "frc2::") {
		t.Errorf("labels/variant:\n%s", j.Body)
	}
}

func TestGitBook(t *testing.T) {
	cs := chunksOf(t, "gitbook.md")
	j := byLang(cs, "Closed Loop Control", "java")
	if j == nil || !strings.Contains(j.Body, "**Warning:**\nClosed loop gains") || !strings.Contains(j.Body, "*Figure: SPARK MAX closed loop diagram*") {
		t.Fatalf("java: %+v", j)
	}
	if strings.Contains(j.Body, "Your First Swerve Robot") || strings.Contains(j.Body, "youtube") {
		t.Errorf("content-ref/embed kept:\n%s", j.Body)
	}
}

func TestReleaseKind(t *testing.T) {
	cs := Chunks(Page{Rel: "revlib/install/changelog", URL: "https://x.invalid/c", Text: "# Changelog\n\n## 2026.0.1\n\nFixed a bug in the SPARK Flex velocity filter configuration."},
		testSrc, "r", time.Unix(0, 0))
	if len(cs) == 0 || cs[0].Kind != "release" {
		t.Fatalf("%+v", cs)
	}
}

func FuzzNormalize(f *testing.F) {
	for _, s := range []string{"```{eval-rst}\n.. code-block:: java\n\n  x\n```", ":::{note}\nx\n:::", "=== \"A\"\n    === \"B\"\n        x", "{% tabs %}\n{% tab title=\"J\" %}"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		_ = sections(Normalize(s))
	})
}

func TestUpstreamSlips(t *testing.T) {
	md := "# Sim\n\n## Lightweight\n\nThe following configuration disables both streams to keep the simulation fast.\n\n.. code-block:: java\n\n     // lightweight config version\n     cameraSim.enableRawStream(false);\n\nAfter the block the prose continues normally here.\n\n!!! tip You can see the waypoints as they were in the last generation.\n\n<iframe width=\"100%\" src=\"https://www.youtube.com/embed/x\"></iframe>\n"
	cs := Chunks(Page{Rel: "sim", URL: "https://x.invalid/sim", Text: md}, testSrc, "r", time.Unix(0, 0))
	j := byLang(cs, "Lightweight", "java")
	if j == nil || !strings.Contains(j.Body, "```java\n// lightweight config version\ncameraSim.enableRawStream(false);\n```") ||
		!strings.Contains(j.Body, "After the block the prose") || !strings.Contains(j.Body, "**Tip:** You can see") ||
		strings.Contains(j.Body, "iframe") || strings.Contains(j.Body, ".. code-block") {
		t.Fatalf("%+v", cs)
	}
}
