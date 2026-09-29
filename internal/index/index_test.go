package index

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/textutil"
)

// OpenFixture builds the fixture shard into a temp dir and opens it.
func openFixture(tb testing.TB) *Reader {
	tb.Helper()
	ctx := context.Background()
	out := filepath.Join(tb.TempDir(), "fixture.sqlite")
	root := filepath.Join("..", "..", "testdata", "fixture")
	if _, err := BuildFromJSONL(ctx, out, "fixture", filepath.Join(root, "chunks.jsonl"), filepath.Join(root, "symbols.jsonl")); err != nil {
		tb.Fatal(err)
	}
	r, err := Open(ctx, out)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { r.Close() })
	return r
}

func TestBuildAndMeta(t *testing.T) {
	r := openFixture(t)
	m := r.Meta()
	if m.Chunks != 20 || m.Symbols != 15 || m.Schema != SchemaVersion || len(m.BuildID) != 32 {
		t.Fatalf("meta = %+v", m)
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	a, b := openFixture(t).Meta().BuildID, openFixture(t).Meta().BuildID
	if a != b {
		t.Fatalf("build ids differ: %s vs %s", a, b)
	}
}

func TestFTSFilters(t *testing.T) {
	r := openFixture(t)
	ctx := context.Background()
	match := textutil.FTSQuery("swerve kinematics")

	all, err := r.FTS(ctx, match, Filter{}, Boost{}, 50, nil)
	if err != nil || len(all) < 3 {
		t.Fatalf("unfiltered: %v %d", err, len(all))
	}
	ids := func(f Filter) map[string]bool {
		hits, err := r.FTS(ctx, match, f, Boost{}, 50, nil)
		if err != nil {
			t.Fatal(err)
		}
		rows := make([]int64, len(hits))
		for i, h := range hits {
			rows[i] = h.Row
		}
		cs, err := r.Chunks(ctx, rows)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, c := range cs {
			out[c.ID()] = true
			if f.Season != "" && c.Season != f.Season {
				t.Errorf("season filter leaked %s (%s)", c.ID(), c.Season)
			}
			if f.Language != "" && c.Language != f.Language && c.Language != "any" {
				t.Errorf("language filter leaked %s (%s)", c.ID(), c.Language)
			}
		}
		return out
	}
	got := ids(Filter{Season: "2026", Language: "java"})
	if !got["wpilib/2026/kinematics/swerve-drive-kinematics#0"] || got["wpilib/2027/kinematics/swerve-drive-kinematics#0"] {
		t.Fatalf("2026 java: %v", got)
	}
	if got := ids(Filter{Season: "2027"}); !got["wpilib/2027/kinematics/swerve-drive-kinematics#0"] {
		t.Fatalf("2027: %v", got)
	}
	if got := ids(Filter{Language: "cpp"}); !got["wpilib/2026/kinematics/swerve-drive-kinematics-cpp#0"] {
		t.Fatalf("cpp: %v", got)
	}
}

func TestCommunityExcludedByDefault(t *testing.T) {
	r := openFixture(t)
	ctx := context.Background()
	match := textutil.FTSQuery("CAN utilization errors")
	hits, _ := r.FTS(ctx, match, Filter{}, Boost{}, 50, nil)
	cs, _ := r.Chunks(ctx, rowsOf(hits))
	for _, c := range cs {
		if c.Trust == "community" {
			t.Fatalf("community chunk %s returned without opt-in", c.ID())
		}
	}
	hits, _ = r.FTS(ctx, match, Filter{IncludeCommunity: true}, Boost{}, 50, nil)
	cs, _ = r.Chunks(ctx, rowsOf(hits))
	var suspect, clean bool
	for _, c := range cs {
		if c.Trust == "community" {
			suspect = suspect || c.Suspect
			clean = clean || !c.Suspect
		}
	}
	if !suspect || !clean {
		t.Fatalf("expected one suspect and one clean community chunk; suspect=%v clean=%v", suspect, clean)
	}
}

func TestSymbolLookup(t *testing.T) {
	r := openFixture(t)
	ctx := context.Background()
	for _, tc := range []struct {
		q       SymbolQuery
		wantFQN string
		wantN   int
	}{
		{SymbolQuery{Name: "SwerveDriveKinematics", Season: "2026", Language: "java"}, "edu.wpi.first.math.kinematics.SwerveDriveKinematics", 1},
		{SymbolQuery{Name: "SwerveDriveKinematics", Season: "2027"}, "org.wpilib.math.kinematics.SwerveDriveKinematics", 1},
		{SymbolQuery{Name: "swervedrivekinematics"}, "", 3}, // case-insensitive, all seasons/languages
		{SymbolQuery{Name: "SwerveDriveKinematics#toSwerveModuleStates"}, "edu.wpi.first.math.kinematics.SwerveDriveKinematics#toSwerveModuleStates", 1},
		{SymbolQuery{Name: "frc::SwerveDriveKinematics"}, "frc::SwerveDriveKinematics", 1},
		{SymbolQuery{Name: "TalonFX#TalonFX"}, "com.ctre.phoenix6.hardware.TalonFX#TalonFX", 2}, // overloads
		{SymbolQuery{Name: "com.revrobotics.CANSparkMax", Season: "2026"}, "", 0},
	} {
		got, err := r.Symbols(ctx, tc.q)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != tc.wantN || (tc.wantFQN != "" && got[0].FQN != tc.wantFQN) {
			t.Errorf("Symbols(%+v) = %d results %v", tc.q, len(got), got)
		}
	}
}

func TestChunkByID(t *testing.T) {
	r := openFixture(t)
	ctx := context.Background()
	c, err := r.ChunkByID(ctx, "phoenix6/2026/motion-magic#0")
	if err != nil || c.Library != "phoenix6" || c.Trust != "vendor" {
		t.Fatalf("got %+v, %v", c, err)
	}
	if _, err := r.ChunkByID(ctx, "phoenix6/2026/motion-magic#9"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := r.ChunkByID(ctx, "no-ordinal"); err == nil {
		t.Fatal("want parse error")
	}
}

func TestWriterRejectsInvalidChunk(t *testing.T) {
	ctx := context.Background()
	w, err := Create(ctx, filepath.Join(t.TempDir(), "x.sqlite"), "x")
	if err != nil {
		t.Fatal(err)
	}
	defer w.abort()
	good := Chunk{DocID: "d", Library: "wpilib", VersionLo: "2026.2.1", Season: "2026", Channel: "stable",
		Language: "java", Kind: "prose", Body: "b", SourceURL: "https://x", UpstreamRev: "r",
		RetrievedAt: time.Unix(1, 0), License: "l", Trust: "official"}
	if err := w.AddChunk(ctx, good); err != nil {
		t.Fatal(err)
	}
	for name, mut := range map[string]func(*Chunk){
		"no license":   func(c *Chunk) { c.License = "" },
		"bad trust":    func(c *Chunk) { c.Trust = "trusted" },
		"http url":     func(c *Chunk) { c.SourceURL = "http://x" },
		"no retrieved": func(c *Chunk) { c.RetrievedAt = time.Time{} },
		"bad language": func(c *Chunk) { c.Language = "kotlin" },
	} {
		c := good
		c.Ord = 1
		mut(&c)
		if err := w.AddChunk(ctx, c); !errors.Is(err, ErrInvalidChunk) {
			t.Errorf("%s: want ErrInvalidChunk, got %v", name, err)
		}
	}
}

func rowsOf(hs []Scored) []int64 {
	out := make([]int64, len(hs))
	for i, h := range hs {
		out[i] = h.Row
	}
	return out
}

func BenchmarkFTS(b *testing.B) {
	r := openFixture(b)
	ctx := context.Background()
	match := textutil.FTSQuery("configure TalonFX Motion Magic")
	var buf []Scored
	b.ReportAllocs()
	for b.Loop() {
		buf, _ = r.FTS(ctx, match, Filter{Season: "2026", Language: "java"}, Boost{Libraries: []string{"phoenix6"}, Symbols: []string{"TalonFX"}}, 50, buf)
	}
}

func BenchmarkSymbol(b *testing.B) {
	r := openFixture(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = r.Symbols(ctx, SymbolQuery{Name: "SwerveDriveKinematics", Season: "2026", Language: "java"})
	}
}

// STAT4 samples make SQLite re-prepare statements whenever a bound value
// changes (every search); shards must ship stat1 only.
func TestNoStat4Samples(t *testing.T) {
	r := openFixture(t)
	var n int
	if err := r.db.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_master WHERE name = 'sqlite_stat4'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 1 {
		if err := r.db.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_stat4`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Fatalf("sqlite_stat4 has %d rows", n)
		}
	}
	if err := r.db.QueryRowContext(context.Background(), `SELECT count(*) FROM sqlite_stat1`).Scan(&n); err != nil || n == 0 {
		t.Fatalf("stat1 rows %d err %v", n, err)
	}
}

func TestLicensesCoverSymbolsAndFacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "l.sqlite")
	w, err := Create(ctx, path, "l")
	if err != nil {
		t.Fatal(err)
	}
	if err := w.AddSymbol(ctx, Symbol{FQN: "a.B", Library: "x", Version: "1", Season: "2026", Language: "java", Kind: "class",
		Signature: "class B", SourceURL: "https://x", UpstreamRev: "r", RetrievedAt: time.Unix(1, 0), License: "LicenseRef-X", Trust: "vendor"}); err != nil {
		t.Fatal(err)
	}
	if err := w.AddHWSpec(ctx, HWSpec{Part: "p", Name: "P", Category: "motor", Source: "s", Season: "2026", Fields: map[string]float64{"a_v": 1},
		SourceURL: "https://x", License: "MIT", Trust: "vendor"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := r.Licenses(ctx)
	if err != nil || len(got) != 2 || got[0] != "LicenseRef-X" || got[1] != "MIT" {
		t.Fatalf("licenses = %v, %v", got, err)
	}
}

// The simple-name column is NOCASE: an exact-case match drops the rows that
// differ only in case; without one they are kept and marked.
func TestSymbolLookupPrefersCase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "c.sqlite")
	w, err := Create(ctx, path, "c")
	if err != nil {
		t.Fatal(err)
	}
	for _, fqn := range []string{"com.ctre.phoenix6.hardware.TalonFX", "swervelib.motors.MotorType#TALONFX"} {
		if err := w.AddSymbol(ctx, Symbol{FQN: fqn, Library: "x", Version: "1", Season: "2026", Language: "java", Kind: "class",
			Signature: "s", SourceURL: "https://x", UpstreamRev: "r", RetrievedAt: time.Unix(1, 0), License: "MIT", Trust: "vendor"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for _, tc := range []struct {
		name     string
		want     []string
		mismatch bool
	}{
		{"TalonFX", []string{"com.ctre.phoenix6.hardware.TalonFX"}, false},
		{"TALONFX", []string{"swervelib.motors.MotorType#TALONFX"}, false},
		{"talonfx", []string{"com.ctre.phoenix6.hardware.TalonFX", "swervelib.motors.MotorType#TALONFX"}, true},
		{"MotorType#TALONFX", []string{"swervelib.motors.MotorType#TALONFX"}, false},
		{"MotorType#TalonFX", []string{"swervelib.motors.MotorType#TALONFX"}, true},
	} {
		got, err := r.Symbols(ctx, SymbolQuery{Name: tc.name, Season: "2026"})
		if err != nil {
			t.Fatal(err)
		}
		var fqns []string
		for _, s := range got {
			fqns = append(fqns, s.FQN)
			if s.CaseMismatch != tc.mismatch {
				t.Errorf("%s: %s CaseMismatch=%v", tc.name, s.FQN, s.CaseMismatch)
			}
		}
		slices.Sort(fqns)
		if !slices.Equal(fqns, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, fqns, tc.want)
		}
	}
}
