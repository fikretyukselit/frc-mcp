package textutil

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-cmp/cmp"
)

func TestSplitIdent(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"TalonFXConfiguration", []string{"talon", "fx", "configuration"}},
		{"toSwerveModuleStates", []string{"to", "swerve", "module", "states"}},
		{"SwerveDriveKinematics", []string{"swerve", "drive", "kinematics"}},
		{"kMaxRPM_2", []string{"k", "max", "rpm", "2"}},
		{"SparkMax", []string{"spark", "max"}},
		{"CANSparkMax", []string{"can", "spark", "max"}},
		{"getPosition", []string{"get", "position"}},
		{"snake_case_name", []string{"snake", "case", "name"}},
		{"motor", nil},
		{"ID", nil},
		{"NEO550", []string{"neo", "550"}},
	} {
		if got := SplitIdent(tc.in); !cmp.Equal(got, tc.want) {
			t.Errorf("SplitIdent(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestFTSQuery(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"How do I configure a TalonFX?", `"configure" OR ("talonfx" OR ("talon" "fx"))`},
		{`edu.wpi.first.math.kinematics.SwerveDriveKinematics`,
			`"edu" OR "wpi" OR "first" OR "math" OR "kinematics" OR ("swervedrivekinematics" OR ("swerve" "drive" "kinematics"))`},
		{`"; DROP TABLE chunk; --`, `"drop" OR "table" OR "chunk"`},
		{"the a of", ""},
		{"motion magic motion", `"motion" OR "magic"`},
	} {
		if got := FTSQuery(tc.in); got != tc.want {
			t.Errorf("FTSQuery(%q)\n got %s\nwant %s", tc.in, got, tc.want)
		}
	}
}

func TestExpandDedup(t *testing.T) {
	got := Expand("TalonFX talonFX config", "TalonFX")
	if got != "talon fx" {
		t.Fatalf("Expand = %q", got)
	}
}

func TestSnippet(t *testing.T) {
	long := strings.Repeat("word ", 400) + "\n\n" + strings.Repeat("tail ", 400)
	s, cut := Snippet(long, 100)
	if !cut || EstimateTokens(s) > 102 {
		t.Fatalf("cut=%v tokens=%d", cut, EstimateTokens(s))
	}
	code := "```java\n" + strings.Repeat("x();\n", 200) + "```"
	s, _ = Snippet(code, 50)
	if strings.Count(s, "```")%2 != 0 {
		t.Fatalf("unterminated fence: %q", s[len(s)-20:])
	}
	if s, cut := Snippet("short", 10); cut || s != "short" {
		t.Fatal("short text must be returned unchanged")
	}
}

func FuzzSnippet(f *testing.F) {
	f.Add("héllo wörld ```go\nx\n``` done.", 3)
	f.Fuzz(func(t *testing.T, s string, n int) {
		if n < 0 || n > 1<<16 || !utf8.ValidString(s) {
			return
		}
		out, _ := Snippet(s, n)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8 output for %q", s)
		}
	})
}

func FuzzFTSQuery(f *testing.F) {
	f.Add(`a "b" c* NEAR(d e) ^f -g`)
	f.Fuzz(func(t *testing.T, q string) {
		out := FTSQuery(q)
		// Every double quote must be part of a balanced, quoted term.
		if strings.Count(out, `"`)%2 != 0 {
			t.Fatalf("unbalanced quotes in %s", out)
		}
	})
}

func BenchmarkFTSQuery(b *testing.B) {
	for b.Loop() {
		FTSQuery("How do I configure TalonFXConfiguration Motion Magic with SwerveDriveKinematics in 2026?")
	}
}
