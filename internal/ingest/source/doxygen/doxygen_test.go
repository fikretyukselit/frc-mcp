package doxygen

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

const classPage = `<html><body>
<div class="headertitle"><div class="title">frc::XboxController Class Reference<span class="mlabels"><span class="mlabel">final</span></span></div></div>
<div class="contents">
<table class="memberdecls">
<tr class="heading"><td colspan="2"><h2 class="groupheader">Public Member Functions</h2></td></tr>
<tr class="memitem:a1" id="r_a1"><td class="memItemLeft">&#160;</td><td class="memItemRight"><a class="el" href="#a1">XboxController</a> (int port)</td></tr>
<tr class="memdesc:a1"><td class="mdescLeft">&#160;</td><td class="mdescRight">Construct an instance of a controller.  <br /></td></tr>
<tr class="memitem:a2" id="r_a2"><td class="memItemLeft">bool&#160;</td><td class="memItemRight"><a class="el" href="#a2">GetLeftBumper</a> () const</td></tr>
<tr class="memdesc:a2"><td class="mdescLeft">&#160;</td><td class="mdescRight">Read the value of the left bumper.  <br /></td></tr>
<tr class="memitem:a3" id="r_a3"><td class="memItemLeft">bool&#160;</td><td class="memItemRight"><a class="el" href="#a3">GetLeftBumperButton</a> () const</td></tr>
<tr class="inherit_header pub_methods_classfrc_1_1_generic_h_i_d"><td colspan="2">Public Member Functions inherited from <a class="el" href="classfrc_1_1_generic_h_i_d.html">frc::GenericHID</a></td></tr>
<tr class="memitem:b1 inherit pub_methods_classfrc_1_1_generic_h_i_d"><td class="memItemLeft">bool&#160;</td><td class="memItemRight"><a class="el" href="#b1">IsConnected</a> () const</td></tr>
<tr class="memdesc:b1 inherit pub_methods_classfrc_1_1_generic_h_i_d"><td class="mdescLeft">&#160;</td><td class="mdescRight">Inherited description.  <br /></td></tr>
<tr class="heading"><td colspan="2"><h2 class="groupheader">Private Attributes</h2></td></tr>
<tr class="memitem:c1" id="r_c1"><td class="memItemLeft">int&#160;</td><td class="memItemRight"><a class="el" href="#c1">m_port</a></td></tr>
</table>
<div class="textblock"><p>Handle input from Xbox controllers connected to the Driver Station. More text.</p></div>
</div></body></html>`

const deprecatedPage = `<html><body><dl class="reflist">
<dt>Member <a class="el" href="classfrc_1_1_xbox_controller.html#a2">frc::XboxController::GetLeftBumper</a> () const</dt>
<dd>Use GetLeftBumperButton instead.</dd>
</dl></body></html>`

func TestParse(t *testing.T) {
	p := filepath.Join(t.TempDir(), "doc.zip")
	fh, _ := os.Create(p)
	zw := zip.NewWriter(fh)
	for name, body := range map[string]string{
		"classfrc_1_1_xbox_controller.html":         classPage,
		"classfrc_1_1_xbox_controller-members.html": classPage,
		"classfmt_1_1_formatter.html":               `<div class="title">fmt::formatter Class Reference</div>`,
		"classfrc_1_1detail_1_1_x.html":             `<div class="title">frc::detail::X Class Reference</div>`,
		"deprecated.html":                           deprecatedPage,
	} {
		w, _ := zw.Create(name)
		_, _ = w.Write([]byte(body))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	src := sources.Source{Library: "wpilib", Season: "2026", Channel: "stable", Version: "2026.2.2", License: "BSD-3-Clause",
		Trust: "official", BaseURL: "https://x/cpp/", Include: "frc::,frc2::"}
	syms := map[string]index.Symbol{}
	var chunks []index.Chunk
	st, err := Parse(p, src, "r", time.Unix(0, 0), func(s index.Symbol) error { syms[s.FQN] = s; return nil },
		func(c index.Chunk) error { chunks = append(chunks, c); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if st.Types != 1 || st.Members != 3 || st.Deprecated != 1 {
		t.Fatalf("stats %+v", st)
	}
	c := syms["frc::XboxController"]
	if c.Signature != "class frc::XboxController : public frc::GenericHID" || c.Summary != "Handle input from Xbox controllers connected to the Driver Station." {
		t.Errorf("class %+v", c)
	}
	if k := syms["frc::XboxController#XboxController"]; k.Kind != "constructor" || k.Signature != "XboxController (int port)" {
		t.Errorf("ctor %+v", k)
	}
	d := syms["frc::XboxController#GetLeftBumper"]
	if d.DeprecatedIn != "2026.2.2" || d.Summary != "Deprecated. Use GetLeftBumperButton instead. Read the value of the left bumper." ||
		d.SourceURL != "https://x/cpp/classfrc_1_1_xbox_controller.html#a2" {
		t.Errorf("deprecated %+v", d)
	}
	if b := syms["frc::XboxController#GetLeftBumperButton"]; b.Summary != "" {
		t.Errorf("inherited description leaked: %+v", b)
	}
	if _, ok := syms["frc::XboxController#IsConnected"]; ok {
		t.Error("inherited member listed as own")
	}
	if _, ok := syms["frc::XboxController#m_port"]; ok {
		t.Error("private member kept")
	}
	if len(chunks) != 1 || chunks[0].Language != "cpp" || chunks[0].DocID != "wpilib-cpp/2026/frc::XboxController" {
		t.Errorf("chunks %+v", chunks)
	}
}
