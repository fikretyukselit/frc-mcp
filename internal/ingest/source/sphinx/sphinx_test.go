package sphinx

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

const page = `<html><body>%s
<section id="swerve-drive-kinematics"><h1>Swerve Drive Kinematics<a class="headerlink" href="#x">¶</a></h1>
<p>The <code class="docutils literal"><span class="pre">SwerveDriveKinematics</span></code> class converts <strong>ChassisSpeeds</strong>.</p>
<div class="admonition note"><p class="admonition-title">Note</p><p>Angles are CCW positive.</p></div>
<section id="constructing"><h2>Constructing the kinematics object</h2>
<p>Pass the module locations relative to the robot center to the constructor.</p>
<div class="sd-tab-set docutils">
<input type="radio"><label class="sd-tab-label" data-sync-id="java">JAVA</label><div class="sd-tab-content docutils">
<div class="highlight-java notranslate"><div class="highlight"><pre>var k = new SwerveDriveKinematics(fl, fr, bl, br);</pre></div></div></div>
<input type="radio"><label class="sd-tab-label" data-sync-id="c++">C++</label><div class="sd-tab-content docutils">
<div class="highlight-c++ notranslate"><div class="highlight"><pre>frc::SwerveDriveKinematics&lt;4&gt; k{fl, fr, bl, br};</pre></div></div></div>
</div>
<ul><li>First item with <code>code</code></li><li>Second item</li></ul>
</section></section>%s</body></html>`

func buildZip(t *testing.T, wrapOpen, wrapClose string) string {
	p := filepath.Join(t.TempDir(), "docs.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	body := []byte(strings.Replace(strings.Replace(page, "%s", wrapOpen, 1), "%s", wrapClose, 1))
	for _, name := range []string{"frc-docs/docs/software/kinematics/swerve.html", "frc-docs/docs/contributing/ignored.html"} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(body); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return p
}

func TestParseBothThemes(t *testing.T) {
	src := sources.Source{ZipRoot: "frc-docs/docs/", BaseURL: "https://docs.wpilib.org/en/stable/docs/", Library: "wpilib",
		Season: "2026", Channel: "stable", Version: "2026", License: "CC-BY-4.0", Trust: "official"}
	for theme, wrap := range map[string][2]string{
		"rtd":  {`<div itemprop="articleBody">`, `</div>`},
		"furo": {`<article role="main" id="furo-main-content">`, `</article>`},
	} {
		t.Run(theme, func(t *testing.T) {
			var got []index.Chunk
			st, err := Parse(buildZip(t, wrap[0], wrap[1]), src, "rev", time.Unix(1, 0), func(c index.Chunk) error { got = append(got, c); return nil })
			if err != nil || st.Pages != 1 {
				t.Fatalf("stats=%+v err=%v", st, err)
			}
			byLang := map[string]index.Chunk{}
			for _, c := range got {
				byLang[c.Language] = c
				if err := c.Validate(); err != nil && !strings.Contains(err.Error(), "retrieved_at") {
					t.Errorf("invalid chunk: %v", err)
				}
			}
			java, cpp, anyc := byLang["java"], byLang["cpp"], byLang["any"]
			if !strings.Contains(java.Body, "new SwerveDriveKinematics(") || strings.Contains(java.Body, "frc::") {
				t.Errorf("java variant wrong:\n%s", java.Body)
			}
			if !strings.Contains(cpp.Body, "frc::SwerveDriveKinematics<4>") || strings.Contains(cpp.Body, "new SwerveDriveKinematics") {
				t.Errorf("cpp variant wrong:\n%s", cpp.Body)
			}
			if !strings.Contains(java.Body, "Pass the module locations") || !strings.Contains(java.Body, "- First item with `code`") {
				t.Errorf("shared prose/list missing from java variant:\n%s", java.Body)
			}
			if java.HeadingPath != "Swerve Drive Kinematics › Constructing the kinematics object" || java.Anchor != "constructing" {
				t.Errorf("heading path/anchor: %q %q", java.HeadingPath, java.Anchor)
			}
			if !strings.Contains(anyc.Body, "> **Note:** Angles are CCW positive.") || strings.Contains(anyc.Body, "¶") {
				t.Errorf("admonition/headerlink handling:\n%s", anyc.Body)
			}
			if !strings.HasPrefix(java.SourceURL, "https://docs.wpilib.org/en/stable/docs/software/kinematics/swerve.html") {
				t.Errorf("source url %s", java.SourceURL)
			}
		})
	}
}
