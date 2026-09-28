package javadoc

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

// classPage mirrors the JDK 17+ doclet structure used by WPILib's Javadoc.
const classPage = `<html><body><main>
<section class="class-description" id="class-description">
<div class="type-signature"><span class="modifiers">public class </span><span class="element-name">XboxController</span>
<span class="extends-implements">extends GenericHID</span></div>
<div class="block">Handle input from Xbox controllers connected to the Driver Station. This class handles Xbox input.</div>
</section>
<section class="details"><ul>
<li><section class="constructor-details" id="constructor-detail"><h2>Constructor Details</h2><ul><li>
<section class="detail" id="&lt;init&gt;(int)"><h3>XboxController</h3>
<div class="member-signature"><span class="modifiers">public</span>&nbsp;<span class="element-name">XboxController</span><span class="parameters">(int&nbsp;port)</span></div>
<div class="block">Construct an instance of a controller.</div></section></li></ul></section></li>
<li><section class="method-details" id="method-detail"><h2>Method Details</h2><ul>
<li><section class="detail" id="getLeftY()"><h3>getLeftY</h3>
<div class="member-signature"><span class="modifiers">public</span>&nbsp;<span class="return-type">double</span>&nbsp;<span class="element-name">getLeftY</span>()</div>
<div class="block">Get the Y axis value of left side of the controller. Right is positive.</div></section></li>
<li><section class="detail" id="getLeftBumper()"><h3>getLeftBumper</h3>
<div class="member-signature"><span class="annotations">@Deprecated(since="2025", forRemoval=true)
</span><span class="modifiers">public</span>&nbsp;<span class="return-type">boolean</span>&nbsp;<span class="element-name">getLeftBumper</span>()</div>
<div class="deprecation-block"><span class="deprecated-label">Deprecated, for removal: This API element is subject to removal in a future version.</span>
<div class="deprecation-comment">Use <a href="#getLeftBumperButton()"><code>getLeftBumperButton()</code></a> instead.</div></div>
<div class="block">Read the value of the left bumper (LB) button on the controller.</div></section></li>
<li><section class="detail" id="setRumble(double)"><h3>setRumble</h3>
<div class="member-signature"><span class="modifiers">public</span>&nbsp;<span class="return-type">void</span>&nbsp;<span class="element-name">setRumble</span>(double&nbsp;value)</div>
<div class="block">Description copied from class: GenericHID Set the rumble output for the HID.</div></section></li>
</ul></section></li></ul></section></main></body></html>`

func TestParse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "doc.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for name, body := range map[string]string{
		"type-search-index.js":                      `typeSearchIndex = [{"p":"edu.wpi.first.wpilibj","l":"XboxController"},{"l":"All Classes","u":"allclasses-index.html"}];updateSearchResults();`,
		"edu/wpi/first/wpilibj/XboxController.html": classPage,
	} {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()

	src := sources.Source{BaseURL: "https://github.wpilib.org/allwpilib/docs/release/java/", Library: "wpilib",
		Language: "java", Season: "2026", Channel: "stable", Version: "2026.2.2", License: "BSD-3-Clause", Trust: "official"}
	syms := map[string][]index.Symbol{}
	var chunks []index.Chunk
	st, err := Parse(p, src, "rev", time.Unix(1, 0),
		func(s index.Symbol) error { syms[s.FQN] = append(syms[s.FQN], s); return nil },
		func(c index.Chunk) error { chunks = append(chunks, c); return nil })
	if err != nil || st.Types != 1 || st.Members != 4 || st.Deprecated != 1 {
		t.Fatalf("stats=%+v err=%v", st, err)
	}
	cls := syms["edu.wpi.first.wpilibj.XboxController"][0]
	if cls.Kind != "class" || cls.Signature != "public class XboxController extends GenericHID" ||
		cls.Summary != "Handle input from Xbox controllers connected to the Driver Station." {
		t.Errorf("class symbol: %+v", cls)
	}
	if c := syms["edu.wpi.first.wpilibj.XboxController#XboxController"]; len(c) != 1 || c[0].Kind != "constructor" {
		t.Errorf("constructor: %+v", c)
	}
	dep := syms["edu.wpi.first.wpilibj.XboxController#getLeftBumper"][0]
	if dep.DeprecatedIn != "2025" || dep.Replacement != "getLeftBumperButton()" || !strings.HasPrefix(dep.Summary, "Deprecated for removal.") {
		t.Errorf("deprecation: %+v", dep)
	}
	if !strings.HasSuffix(dep.SourceURL, "XboxController.html#getLeftBumper()") {
		t.Errorf("anchor url: %s", dep.SourceURL)
	}
	if s := syms["edu.wpi.first.wpilibj.XboxController#setRumble"][0].Summary; s != "Set the rumble output for the HID." {
		t.Errorf("copied-description preamble not stripped: %q", s)
	}
	if len(chunks) != 1 || chunks[0].Kind != "api" || chunks[0].Symbol != "edu.wpi.first.wpilibj.XboxController" ||
		!strings.Contains(chunks[0].Body, "`public double getLeftY()`") || !strings.Contains(chunks[0].Body, "**(deprecated)**") {
		t.Errorf("chunk: %+v", chunks)
	}
	if chunks[0].ID() != cls.ChunkID {
		t.Errorf("symbol → chunk link broken: %s vs %s", cls.ChunkID, chunks[0].ID())
	}
}
