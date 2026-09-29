package cppheader

import (
	"archive/zip"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// zipDir packs testdata/headers into a headers.zip the way vendor Maven
// artifacts are laid out (include paths at the archive root).
func zipDir(t *testing.T, dir string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "headers.zip")
	fh, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(fh)
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		w, err := zw.Create(filepath.ToSlash(rel))
		if err != nil {
			return err
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}

type parsed struct {
	st     Stats
	syms   map[string][]index.Symbol
	chunks []index.Chunk
}

func parseTestdata(t *testing.T, include string, skip ...string) parsed {
	t.Helper()
	src := sources.Source{Library: "acme", Season: "2026", Channel: "stable", Version: "26.3.0", License: "MIT",
		Trust: "vendor", BaseURL: "https://docs.example/cpp/", Include: include, Skip: skip}
	out := parsed{syms: map[string][]index.Symbol{}}
	var err error
	out.st, err = Parse(zipDir(t, "testdata/headers"), src, "etag1", time.Unix(0, 0),
		func(s index.Symbol) error { out.syms[s.FQN] = append(out.syms[s.FQN], s); return nil },
		func(c index.Chunk) error { out.chunks = append(out.chunks, c); return nil })
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestParseSymbols(t *testing.T) {
	p := parseTestdata(t, "acme::", "acme/internal_ids/")
	one := func(fqn string) index.Symbol {
		t.Helper()
		s := p.syms[fqn]
		if len(s) != 1 {
			t.Fatalf("%s: %d symbols, want 1 (%+v)", fqn, len(s), s)
		}
		return s[0]
	}
	const motorPage = "https://docs.example/cpp/classacme_1_1motor_1_1_motor.html"
	tests := []struct {
		fqn, kind, sig, summary, url string
	}{
		{"acme::motor::DeviceBase", "class", "class acme::motor::DeviceBase", "Common base of every device.",
			"https://docs.example/cpp/classacme_1_1motor_1_1_device_base.html"},
		{"acme::motor::DeviceBase#RefreshAll", "method", "static int RefreshAll ()", "Refreshes all devices.", ""},
		{"acme::motor::DeviceBase#OnUpdate", "method", "virtual void OnUpdate ()", "Hook for subclasses.", ""},
		{"acme::motor::core::CoreMotor", "class", "class acme::motor::core::CoreMotor : public acme::motor::DeviceBase",
			"Generated device core.", ""},
		{"acme::motor::core::CoreMotor#CoreMotor", "constructor", `CoreMotor (int id, std::string canbus = "")`, "", ""},
		{"acme::motor::core::CoreMotor#GetPosition", "method", "double GetPosition () const", "Gets the position.", ""},
		{"acme::motor::core::CoreMotor#WaitForAll", "method", "static int WaitForAll (double timeout, Signals &... signals)", "", ""},
		{"acme::motor::Motor", "class",
			"class acme::motor::Motor : public acme::motor::core::CoreMotor, public other::Sendable", "Motor controller.", motorPage},
		{"acme::motor::Motor#None", "method", "static Motor None ()", "", motorPage},
		{"acme::motor::Motor#Set", "method", "void Set (double speed)", "", ""},
		{"acme::motor::Motor#kMaxId", "field", "static constexpr int kMaxId = 62", "", ""},
		{"acme::motor::Motor#speed", "field", "double speed = 0.0", "", ""},
		{"acme::motor::Motor#WindowsOnly", "method", "void WindowsOnly ()", "", ""}, // first #if branch
		{"acme::motor::Motor#Protect", "method", "ACMEEXPORT void Protect (int x) override", "", ""},
		{"acme::motor::NeutralModeValue", "struct", "struct acme::motor::NeutralModeValue", "Neutral behavior (an enum-like struct).",
			"https://docs.example/cpp/structacme_1_1motor_1_1_neutral_mode_value.html"},
		{"acme::motor::NeutralModeValue#Brake", "field", "static constexpr int Brake = 1", "", ""},
		{"acme::motor::NeutralModeValue#value", "field", "int value", "", ""},
		{"acme::motor::NeutralModeValue#NeutralModeValue", "constructor", "constexpr NeutralModeValue (int value)", "", ""},
		{"acme::motor::Mode", "enum", "enum class Mode : int { kA, kB, kC }", "",
			"https://docs.example/cpp/namespaceacme_1_1motor.html"},
		{"acme::motor::Mode#kC", "field", "Mode::kC", "", ""},
		{"acme::motor::FeedEnable", "function", "ACMEEXPORT void FeedEnable (int timeoutMs)", "Free function at namespace scope.",
			"https://docs.example/cpp/namespaceacme_1_1motor.html"},
		{"acme::motor::kVersion", "field", "inline constexpr double kVersion = 26.3", "", ""},
		{"acme::motor::Speed", "type", "using Speed = double", "", ""},
		{"acme::spark::SparkBase", "class", "class acme::spark::SparkBase", "", ""},
		{"acme::spark::SparkBase#IdleMode", "type", "enum class IdleMode { kCoast, kBrake }", "", ""},
		{"acme::spark::SparkBase::IdleMode", "enum", "enum class IdleMode { kCoast, kBrake }", "",
			"https://docs.example/cpp/classacme_1_1spark_1_1_spark_base.html"},
		{"acme::spark::SparkBase::IdleMode#kBrake", "field", "IdleMode::kBrake", "", ""},
		{"acme::spark::SparkBase::Faults", "struct", "struct acme::spark::SparkBase::Faults", "",
			"https://docs.example/cpp/structacme_1_1spark_1_1_spark_base_1_1_faults.html"},
		{"acme::spark::SparkBase::Faults#rawBits", "field", "int rawBits", "", ""},
		{"acme::spark::SparkBase#Faults", "type", "struct Faults", "", ""}, // nested types are members too
		{"acme", "namespace", "namespace acme", "", "https://docs.example/cpp/namespaceacme.html"},
		{"acme::spark", "namespace", "namespace acme::spark", "", ""}, // opened as "namespace acme::spark {"
		{"acme::motor::core", "namespace", "namespace acme::motor::core", "", ""},
		{"acme::spark::SparkBase#Callback", "type", "typedef void(*Callback)(int)", "", ""},
		{"acme::spark::SparkBase#Handle", "type", "using Handle = int32_t", "", ""},
		{"acme::spark::SparkBase#GetValue", "method", "int GetValue () const noexcept", "", ""},
		{"acme::spark::SparkBase#GetPair", "method", "auto GetPair () -> std::pair<int, int>", "", ""},
		{"acme::spark::SparkConfig#kBrake", "field", "IdleMode kBrake", "", ""}, // unscoped enumerator
		{"acme::spark::SparkConfig#SetIdleMode", "method", "SparkConfig &SetIdleMode (IdleMode mode)", "", ""},
		{"acme::spark::SparkMax", "class", "class acme::spark::SparkMax : public acme::spark::SparkBase", "", ""},
		{"acme::spark::SparkMax#SparkMax", "constructor", "SparkMax (int id)", "", ""},
	}
	for _, tc := range tests {
		s := one(tc.fqn)
		if s.Kind != tc.kind || s.Signature != tc.sig || s.Summary != tc.summary {
			t.Errorf("%s:\n got  %q %q %q\n want %q %q %q", tc.fqn, s.Kind, s.Signature, s.Summary, tc.kind, tc.sig, tc.summary)
		}
		if tc.url != "" && s.SourceURL != tc.url {
			t.Errorf("%s: url %q, want %q", tc.fqn, s.SourceURL, tc.url)
		}
		if s.Library != "acme" || s.Season != "2026" || s.Version != "26.3.0" || s.Language != "cpp" ||
			s.License != "MIT" || s.Trust != "vendor" || s.UpstreamRev != "etag1" {
			t.Errorf("%s: provenance %+v", tc.fqn, s)
		}
	}

	// Overloads: the deprecated one carries the attribute message.
	var dep, plain int
	for _, s := range p.syms["acme::motor::Motor#Motor"] {
		switch {
		case s.DeprecatedIn == "26.3.0" && s.Summary == "Deprecated. Constructing with a name is deprecated in 2027." &&
			s.Signature == "Motor (int id, std::string name)":
			dep++
		case s.DeprecatedIn == "" && s.Summary == "Constructs a motor." && s.Signature == "explicit Motor (int id)":
			plain++
		}
	}
	if dep != 1 || plain != 1 {
		t.Errorf("Motor constructors: %+v", p.syms["acme::motor::Motor#Motor"])
	}
	if s := one("acme::spark::SparkBase#Configure"); s.DeprecatedIn == "" || s.Summary != "Deprecated. Configure the controller." {
		t.Errorf("@deprecated without text: %+v", s)
	}

	for _, absent := range []string{
		"acme::motor::ParentDevice",        // forward declaration only
		"acme::motor::DeviceBase#m_secret", // private
		"acme::motor::DeviceBase#Hidden",
		"acme::motor::DeviceBase#DeviceBase", // destructor
		"acme::motor::core::CoreMotor#m_pos",
		"acme::motor::Motor#Copy",       // = delete
		"acme::motor::Motor#NotWindows", // #else branch
		"acme::motor::Motor::Broken",    // #if 0
		"acme::motor::detail::Internal",
		"acme::motor::detail",
		"other",
		"acme::spark::SparkBase#Hidden",
		"acme::motor::Anonymous",
		"acme::motor::volts_per_turn", // inside a macro definition
		"acme::spark::SparkBase#SparkMax",
		"acme::spark::SparkBase::Hidden",
		"acme::spark::SparkMax#m_id",
		"acme::SkippedIds", // Skip prefix
		"acme::GeneratedMessage",
		"other::Sendable", // outside Include
		"photon::PhotonCamera",
		"wpi::Struct",
	} {
		if _, ok := p.syms[absent]; ok {
			t.Errorf("%s should not be emitted", absent)
		}
	}
	for fqn := range p.syms {
		if strings.Contains(fqn, "operator") {
			t.Errorf("operator emitted: %s", fqn)
		}
	}
	if p.st.Files != 3 || p.st.Deprecated != 2 {
		t.Errorf("stats %+v", p.st)
	}
}

func TestParseChunks(t *testing.T) {
	p := parseTestdata(t, "acme::")
	var motor *index.Chunk
	for i, c := range p.chunks {
		if c.Language != "cpp" || c.Kind != "api" || c.Library != "acme" || c.Season != "2026" || c.Channel != "stable" ||
			c.License != "MIT" || c.Trust != "vendor" || c.SourceURL == "" || c.DocID != "acme-cpp/2026/"+c.Symbol {
			t.Errorf("chunk provenance %+v", c)
		}
		if c.Symbol == "acme::motor::Motor" {
			motor = &p.chunks[i]
		}
	}
	if motor == nil {
		t.Fatal("no Motor chunk")
	}
	for _, want := range []string{
		"```cpp\nclass acme::motor::Motor : public acme::motor::core::CoreMotor, public other::Sendable\n```",
		"Motor controller.",
		"- `Motor (int id, std::string name)` **(deprecated)**",
		"- `explicit Motor (int id)` — Constructs a motor.",
	} {
		if !strings.Contains(motor.Body, want) {
			t.Errorf("Motor chunk lacks %q:\n%s", want, motor.Body)
		}
	}
	if motor.Title != "Motor (class)" || motor.HeadingPath != "C++ API › acme::motor" {
		t.Errorf("chunk title %q path %q", motor.Title, motor.HeadingPath)
	}
	// Symbols point at their type's first chunk.
	if s := p.syms["acme::motor::Motor#Set"][0]; s.ChunkID != "acme-cpp/2026/acme::motor::Motor#0" {
		t.Errorf("chunk id %q", s.ChunkID)
	}
}

// PhotonCamera.h is a verbatim PhotonLib v2026.3.4 header (MIT; the license
// notice is kept in the file).
func TestParseRecordedPhotonHeader(t *testing.T) {
	p := parseTestdata(t, "photon::")
	for fqn, kind := range map[string]string{
		"photon::PhotonCamera":                     "class",
		"photon::PhotonCamera#GetAllUnreadResults": "method",
		"photon::PhotonCamera#SetPipelineIndex":    "method",
		"photon::PhotonCamera#CameraMatrix":        "type",
		"photon::PhotonCamera#PhotonCamera":        "constructor",
		"photon::LEDMode":                          "enum",
		"photon::LEDMode#kBlink":                   "field",
	} {
		ss := p.syms[fqn]
		if len(ss) == 0 || ss[0].Kind != kind {
			t.Errorf("%s: %+v, want kind %s", fqn, ss, kind)
		}
	}
	if s := p.syms["photon::PhotonCamera#GetLatestResult"]; len(s) != 1 || s[0].DeprecatedIn == "" {
		t.Errorf("GetLatestResult should be deprecated: %+v", s)
	}
	if len(p.syms["photon::PhotonCamera#PhotonCamera"]) != 3 {
		t.Errorf("PhotonCamera constructors: %+v", p.syms["photon::PhotonCamera#PhotonCamera"])
	}
	for fqn := range p.syms {
		if !strings.HasPrefix(fqn, "photon::") && fqn != "photon" {
			t.Errorf("outside include: %s", fqn)
		}
	}
}

func TestDoxyEscape(t *testing.T) {
	for in, want := range map[string]string{
		"ctre::phoenix6::hardware::TalonFX":                   "ctre_1_1phoenix6_1_1hardware_1_1_talon_f_x",
		"ctre::phoenix6::signals::System_StateValue":          "ctre_1_1phoenix6_1_1signals_1_1_system___state_value",
		"rev::spark::SparkBase::Faults":                       "rev_1_1spark_1_1_spark_base_1_1_faults",
		"photon::PhotonPipelineMetadata_PhotonStruct":         "photon_1_1_photon_pipeline_metadata___photon_struct",
		"rev::servohub::ServoChannelConfig::PulseRange_t":     "rev_1_1servohub_1_1_servo_channel_config_1_1_pulse_range__t",
		"ctre::phoenix6::controls::compound::Diff_VoltageOut": "ctre_1_1phoenix6_1_1controls_1_1compound_1_1_diff___voltage_out",
	} {
		if got := doxyEscape(in); got != want {
			t.Errorf("doxyEscape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestDocText(t *testing.T) {
	tests := []struct{ doc, summary, dep string }{
		{"", "", ""},
		{"\\brief Gets the value.\n\nLonger text.", "Gets the value.", ""},
		{"Plain first paragraph. Second.\n\n\\param x unused", "Plain first paragraph.", ""},
		{"\\brief The state of the bridge when\n       output is neutral.\n\\details Extra.", "The state of the bridge when output is neutral.", ""},
		{"Old API.\n\\deprecated Use \\c NewThing instead.\n\\returns x", "Old API.", "Use NewThing instead."},
		{"\\deprecated\n", "", "deprecated"},
		{"\\param a first\n\\returns the <b>sum</b>", "", ""},
		{"@brief Uses at-commands.", "Uses at-commands.", ""},
	}
	for _, tc := range tests {
		s, d := docText(tc.doc)
		if s != tc.summary || d != tc.dep {
			t.Errorf("docText(%q) = %q, %q; want %q, %q", tc.doc, s, d, tc.summary, tc.dep)
		}
	}
}

// FuzzScan checks that the scanner terminates without panicking on any
// input: every loop must consume a token, and a stray '}' must never be
// swallowed by a nested construct.
func FuzzScan(f *testing.F) {
	for _, p := range []string{"testdata/headers/acme/motor/Motor.hpp", "testdata/headers/acme/spark/Spark.h"} {
		b, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(string(b))
	}
	f.Add("class X[ {")
	f.Add("namespace a { Foo() : x ; } }")
	f.Add("template <typename T> requires A<T> && (B) }")
	f.Add("enum class E : int { a = (1 << 2), b, } ;")
	f.Fuzz(func(t *testing.T, s string) {
		p := &parser{t: lex(s), file: "f.h", want: func([]string) (bool, bool) { return true, true }}
		p.parseScope(scope{public: true})
	})
}

func TestLexConditionalsAndDocs(t *testing.T) {
	src := "/** Doc A. */\nint a;\n#if 0\nint dead;\n#else\nint live;\n#endif\n#ifdef X\nint first;\n#elif Y\nint second;\n#else\nint third;\n#endif\n" +
		"/// line doc\nint b; ///< trailing, ignored\n#define M(x) \\\n  int macro_body;\nconst char* s = \"}\"; char c = '}';\n"
	var ids []string
	docs := map[string]string{}
	for _, tk := range lex(src) {
		if tk.k == 'i' && tk.s != "int" && tk.s != "const" && tk.s != "char" {
			ids = append(ids, tk.s)
		}
		if tk.doc != "" {
			docs[tk.s] = tk.doc
		}
	}
	if got := strings.Join(ids, ","); got != "a,live,first,b,s,c" {
		t.Errorf("identifiers %s", got)
	}
	if strings.TrimSpace(docs["int"]) == "" {
		t.Errorf("docs %v", docs)
	}
}
