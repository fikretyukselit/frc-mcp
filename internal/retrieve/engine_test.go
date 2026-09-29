package retrieve_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/router"
	"github.com/fikretyukselit/frc-mcp/internal/testfixture"
)

func TestSearchPipeline(t *testing.T) {
	e := testfixture.Engine(t)
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		q          retrieve.Query
		wantStatus string
		wantTop    string // chunk id of the top hit ("" = no hits)
		wantSeason string
		wantPin    string
	}{
		{"howto vendor", retrieve.Query{Text: "How do I configure Motion Magic on a TalonFX?", Language: "java"},
			retrieve.StatusOK, "phoenix6/2026/motion-magic#0", "2026", "default"},
		{"symbol default season", retrieve.Query{Text: "SwerveDriveKinematics", Language: "java"},
			retrieve.StatusOK, "wpilib/2026/kinematics/swerve-drive-kinematics#0", "2026", "default"},
		{"symbol pinned 2027", retrieve.Query{Text: "SwerveDriveKinematics", Season: "2027"},
			retrieve.StatusOK, "wpilib/2027/kinematics/swerve-drive-kinematics#0", "2027", "arg"},
		{"season from query", retrieve.Query{Text: "what changed in 2027 breaking changes"},
			retrieve.StatusOK, "wpilib/2027/yearly-changelog#0", "2027", "query"},
		{"cpp", retrieve.Query{Text: "frc::SwerveDriveKinematics ToSwerveModuleStates"},
			retrieve.StatusOK, "wpilib/2026/kinematics/swerve-drive-kinematics-cpp#0", "2026", "default"},
		{"python", retrieve.Query{Text: "TimedRobot robotInit", Language: "python"},
			retrieve.StatusOK, "robotpy/2026/timed-robot#0", "2026", "default"},
		{"no match", retrieve.Query{Text: "quantum banana teleportation"},
			retrieve.StatusNoMatch, "", "2026", "default"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.q.Limit = 50 // hydrate everything so the season invariant can be checked on all hits
			r, err := e.Search(ctx, tc.q)
			if err != nil {
				t.Fatal(err)
			}
			if r.Status != tc.wantStatus || r.Season != tc.wantSeason || r.PinSource != tc.wantPin {
				t.Fatalf("status=%s season=%s pin=%s conf=%.2f, want %s/%s/%s", r.Status, r.Season, r.PinSource, r.Confidence, tc.wantStatus, tc.wantSeason, tc.wantPin)
			}
			top := ""
			if len(r.Hits) > 0 {
				top = r.Hits[0].Chunk.ID()
			}
			if top != tc.wantTop {
				t.Fatalf("top = %q, want %q; hits: %v", top, tc.wantTop, ids(r.Hits))
			}
			for _, h := range r.Hits {
				if h.Chunk.Season != r.Season {
					t.Fatalf("wrong-season hit %s (%s) in %s results", h.Chunk.ID(), h.Chunk.Season, r.Season)
				}
			}
		})
	}
}

func TestVersionMismatchNeverBlends(t *testing.T) {
	e := testfixture.Engine(t)
	r, err := e.Search(context.Background(), retrieve.Query{Text: "CANSparkMax setSmartCurrentLimit burnFlash", Season: "2026", Language: "java", Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if r.Status != retrieve.StatusVersionMismatch {
		t.Fatalf("status=%s conf=%.2f hits=%v", r.Status, r.Confidence, ids(r.Hits))
	}
	for _, h := range r.Hits { // weak pinned hits may remain, but only from the pinned season
		if h.Chunk.Season != "2026" {
			t.Fatalf("blended season %s", h.Chunk.ID())
		}
	}
	if len(r.OtherSeason) == 0 || r.OtherSeason[0].Chunk.Season != "2024" {
		t.Fatalf("other-season hits = %v", ids(r.OtherSeason))
	}
}

func TestCommunityPolicy(t *testing.T) {
	e := testfixture.Engine(t)
	ctx := context.Background()
	r, _ := e.Search(ctx, retrieve.Query{Text: "reduce CAN utilization status signals", Limit: 50})
	for _, h := range r.Hits {
		if h.Chunk.Trust == "community" {
			t.Fatalf("community chunk %s without opt-in", h.Chunk.ID())
		}
	}
	// Troubleshooting intent opts in; the suspect post must rank below the clean one.
	r, _ = e.Search(ctx, retrieve.Query{Text: "CAN errors high utilization not working", Limit: 50})
	pos := map[string]int{}
	for i, h := range r.Hits {
		pos[h.Chunk.ID()] = i + 1
	}
	clean, bad := pos["forum/chiefdelphi/can-utilization#0"], pos["forum/chiefdelphi/can-fix-malicious#0"]
	if clean == 0 {
		t.Fatalf("clean forum post missing: %v", ids(r.Hits))
	}
	if bad != 0 && bad < clean {
		t.Fatalf("suspect post ranked above clean one: %v", ids(r.Hits))
	}
}

// Forum posts are opt-in (docs/security.md §2.1): no default search returns
// one, whatever the retriever (BM25, dense, exact symbol) that found it;
// kinds=[forum] returns only forum posts, all trust community.
func TestForumOptIn(t *testing.T) {
	e := testfixture.Engine(t)
	ctx := context.Background()
	for _, q := range []string{
		"reduce CAN utilization status signals",          // general; the forum post is the best lexical match
		"High CAN utilization with 8 Krakens",            // the forum post's exact title
		"BaseStatusSignal.setUpdateFrequencyForAll",      // symbol intent; the post names the method
		"How do I lower CAN bus utilization on Krakens?", // how-to
	} {
		r, err := e.Search(ctx, retrieve.Query{Text: q, Limit: 50})
		if err != nil {
			t.Fatal(err)
		}
		if r.Decision.Intent == router.IntentTroubleshoot {
			t.Fatalf("%q routes to troubleshoot, which opts in by design; use a default-intent query", q)
		}
		for _, h := range append(r.Hits, r.OtherSeason...) {
			if h.Chunk == nil {
				t.Fatalf("%q: hit not hydrated", q)
			}
			if h.Chunk.Kind == "forum" || h.Chunk.Trust == "community" {
				t.Fatalf("%q: forum chunk %s without opt-in", q, h.Chunk.ID())
			}
		}
	}
	r, err := e.Search(ctx, retrieve.Query{Text: "reduce CAN utilization status signals", Kinds: []string{"forum"}, Limit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Hits) == 0 {
		t.Fatal("kinds=[forum] returned nothing")
	}
	for _, h := range r.Hits {
		if h.Chunk == nil || h.Chunk.Kind != "forum" || h.Chunk.Trust != "community" {
			t.Fatalf("kinds=[forum] returned %v", ids(r.Hits))
		}
	}
	if r.Hits[0].Chunk.ID() != "forum/chiefdelphi/can-utilization#0" || r.Hits[0].Chunk.Suspect {
		t.Fatalf("clean post must lead the suspect one: %v", ids(r.Hits))
	}
}

func TestExactSymbolsSurface(t *testing.T) {
	e := testfixture.Engine(t)
	r, _ := e.Search(context.Background(), retrieve.Query{Text: "TalonFXConfiguration"})
	if len(r.Symbols) == 0 || r.Symbols[0].FQN != "com.ctre.phoenix6.configs.TalonFXConfiguration" {
		t.Fatalf("symbols = %+v", r.Symbols)
	}
	if !r.Hits[0].SymbolMatch || r.Confidence < 0.6 {
		t.Fatalf("exact symbol hit should be confident: conf=%.2f match=%v", r.Confidence, r.Hits[0].SymbolMatch)
	}
}

func ids(hs []retrieve.Hit) string {
	var b strings.Builder
	for _, h := range hs {
		if h.Chunk == nil {
			b.WriteString("<light> ")
			continue
		}
		b.WriteString(h.Chunk.ID())
		b.WriteString(" ")
	}
	return b.String()
}

func TestHydrationWindow(t *testing.T) {
	e := testfixture.Engine(t)
	r, _ := e.Search(context.Background(), retrieve.Query{Text: "swerve kinematics module states", Offset: 1, Limit: 1})
	if len(r.Hits) < 3 {
		t.Fatalf("need ≥3 hits, got %d", len(r.Hits))
	}
	if r.Hits[0].Chunk == nil || r.Hits[1].Chunk == nil || r.Hits[2].Chunk != nil {
		t.Fatalf("hydration window wrong: %v", ids(r.Hits))
	}
}

func BenchmarkSearch(b *testing.B) {
	e := testfixture.Engine(b)
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		_, _ = e.Search(ctx, retrieve.Query{Text: "How do I configure Motion Magic on a TalonFX?", Language: "java"})
	}
}
