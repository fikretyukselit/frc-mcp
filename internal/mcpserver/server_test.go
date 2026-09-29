package mcpserver_test

import (
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/mcpserver"
	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
	"github.com/fikretyukselit/frc-mcp/internal/testfixture"
)

var update = flag.Bool("update", false, "rewrite golden files")

func connect(t testing.TB, e *retrieve.Engine) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := mcpserver.New(e, mcpserver.Options{Version: "test",
		Now: func() time.Time { return time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC) }})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.MCP().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close(); ss.Close() })
	return cs
}

func toolsJSON(t *testing.T, cs *mcp.ClientSession) string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(res.Tools, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return string(b) + "\n"
}

// TestToolSurfaceGolden pins the exact tools/list bytes (names, order,
// descriptions, schemas, annotations). Any change must be reviewed.
func TestToolSurfaceGolden(t *testing.T) {
	got := toolsJSON(t, connect(t, testfixture.Engine(t)))
	p := filepath.Join("testdata", "tools_list.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("%v (run go test ./internal/mcpserver -update)", err)
	}
	if string(want) != got {
		t.Fatalf("tools/list changed; review and run -update.\n%s", got)
	}
}

// TestSurfaceIndependentOfIndex: a shard (possibly poisoned) must never change
// what the model is told about the tools (docs/security.md §2.2).
func TestSurfaceIndependentOfIndex(t *testing.T) {
	withIndex := toolsJSON(t, connect(t, testfixture.Engine(t)))
	without := toolsJSON(t, connect(t, nil))
	if withIndex != without {
		t.Fatal("tools/list depends on loaded index data")
	}
}

func TestToolAnnotationsReadOnly(t *testing.T) {
	res, err := connect(t, nil).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	names := []string{}
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
		a := tool.Annotations
		if a == nil || !a.ReadOnlyHint || !a.IdempotentHint || a.DestructiveHint == nil || *a.DestructiveHint {
			t.Errorf("%s: annotations must be read-only/idempotent/non-destructive: %+v", tool.Name, a)
		}
		if tool.OutputSchema == nil {
			t.Errorf("%s: missing output schema", tool.Name)
		}
	}
	// The SDK lists tools sorted by name: deterministic, as SEP-2549 asks.
	if strings.Join(names, ",") != "frc_api,frc_context,frc_fetch,frc_hardware,frc_migrate,frc_search,frc_vendordep,frc_verify_code,frc_whats_new" {
		t.Fatalf("tool order = %v", names)
	}
}

func call(t *testing.T, cs *mcp.ClientSession, name string, args map[string]any) (*mcp.CallToolResult, map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", name, err)
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			text += tc.Text
		}
	}
	var sc map[string]any
	if res.StructuredContent != nil {
		b, _ := json.Marshal(res.StructuredContent)
		_ = json.Unmarshal(b, &sc)
	}
	return res, sc, text
}

func TestSearchEndToEnd(t *testing.T) {
	cs := connect(t, testfixture.Engine(t))
	res, sc, text := call(t, cs, "frc_search", map[string]any{"query": "How do I configure Motion Magic on a TalonFX?", "language": "java"})
	if res.IsError {
		t.Fatalf("unexpected error: %s", text)
	}
	hits, _ := sc["hits"].([]any)
	if sc["status"] != "ok" || sc["frc_season"] != "2026" || len(hits) == 0 {
		t.Fatalf("structured = %v", sc)
	}
	top := hits[0].(map[string]any)
	if top["id"] != "phoenix6/2026/motion-magic#0" || !strings.Contains(text, "phoenix6/2026/motion-magic#0") {
		t.Fatalf("top hit = %v", top["id"])
	}

	// search → fetch round trip
	res, sc, text = call(t, cs, "frc_fetch", map[string]any{"id": top["id"]})
	if res.IsError || !strings.Contains(sc["body"].(string), "MotionMagicVoltage") || !strings.Contains(text, "MotionMagicVoltage") {
		t.Fatalf("fetch: %v %s", res.IsError, text)
	}
}

func TestAPIEndToEnd(t *testing.T) {
	cs := connect(t, testfixture.Engine(t))
	_, sc, text := call(t, cs, "frc_api", map[string]any{"symbol": "CANSparkMax"})
	if sc["status"] != "version_mismatch" || !strings.Contains(text, "removed in 2025.0.0") {
		t.Fatalf("CANSparkMax in 2026 must be version_mismatch: %v\n%s", sc["status"], text)
	}
	_, sc, _ = call(t, cs, "frc_api", map[string]any{"symbol": "SwerveDriveKinematics", "frc_season": "2027", "language": "java"})
	m := sc["matches"].([]any)
	if len(m) != 1 || m[0].(map[string]any)["fqn"] != "org.wpilib.math.kinematics.SwerveDriveKinematics" {
		t.Fatalf("2027 lookup: %v", m)
	}
	if others, _ := sc["other_seasons"].([]any); len(others) == 0 {
		t.Fatal("expected the 2026 edu.wpi.first location in other_seasons")
	}
}

func TestInstructiveErrors(t *testing.T) {
	cs := connect(t, testfixture.Engine(t))
	for _, tc := range []struct {
		tool string
		args map[string]any
		want string
	}{
		{"frc_search", map[string]any{"query": "   "}, "query is required"},
		// Enum/pattern violations are caught by the SDK's schema validator; the
		// message still names the valid values, which is what the model needs.
		{"frc_search", map[string]any{"query": "x", "language": "kotlin"}, "java cpp python any"},
		{"frc_search", map[string]any{"query": "x", "frc_season": "26"}, "^20[2-3][0-9]$"},
		{"frc_search", map[string]any{"query": "x", "cursor": "bogus!"}, "invalid cursor"},
		{"frc_fetch", map[string]any{"uri": "https://evil.example/x"}, "no arbitrary URLs"},
		{"frc_fetch", map[string]any{"id": "nope#0"}, "run frc_search again"},
		{"frc_api", map[string]any{"symbol": ""}, "symbol is required"},
	} {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
		if err != nil {
			// Schema violations are reported by the SDK; they must still be tool errors, not protocol errors.
			t.Errorf("%s %v: protocol error %v", tc.tool, tc.args, err)
			continue
		}
		var text string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				text += tc.Text
			}
		}
		if !res.IsError || !strings.Contains(text, tc.want) {
			t.Errorf("%s %v: isError=%v text=%q, want %q", tc.tool, tc.args, res.IsError, text, tc.want)
		}
	}
}

func TestSyncingWithoutIndex(t *testing.T) {
	_, sc, text := call(t, connect(t, nil), "frc_search", map[string]any{"query": "TalonFX"})
	if sc["status"] != "syncing" || !strings.Contains(text, "not available yet") {
		t.Fatalf("got %v / %s", sc["status"], text)
	}
}

func TestPaginationThroughProtocol(t *testing.T) {
	cs := connect(t, testfixture.Engine(t))
	args := map[string]any{"query": "swerve kinematics module states translation", "k": 1}
	_, sc, _ := call(t, cs, "frc_search", args)
	cur, _ := sc["next_cursor"].(string)
	first := sc["hits"].([]any)[0].(map[string]any)["id"]
	if cur == "" {
		t.Fatal("expected next_cursor")
	}
	args["cursor"] = cur
	_, sc, _ = call(t, cs, "frc_search", args)
	second := sc["hits"].([]any)[0].(map[string]any)["id"]
	if second == first {
		t.Fatalf("page 2 repeated page 1: %v", first)
	}
}

func TestContextPinFlow(t *testing.T) {
	cs := connect(t, testfixture.Engine(t))
	_, sc, text := call(t, cs, "frc_context", map[string]any{"project_root": "../project/testdata/java2026"})
	pin, _ := sc["pin"].(string)
	if sc["frc_season"] != "2026" || sc["language"] != "java" || pin == "" || !strings.Contains(text, "frcYear=2025") {
		t.Fatalf("context: %v\n%s", sc, text)
	}
	// The pin drives search: season and language come from the handle.
	_, sc, _ = call(t, cs, "frc_search", map[string]any{"query": "SwerveDriveKinematics", "pin": pin})
	if sc["frc_season"] != "2026" || sc["pin_source"] != "handle" || sc["language"] != "java" {
		t.Fatalf("pinned search: season=%v pin_source=%v lang=%v", sc["frc_season"], sc["pin_source"], sc["language"])
	}
	// An explicit argument still wins over the handle.
	_, sc, _ = call(t, cs, "frc_search", map[string]any{"query": "SwerveDriveKinematics", "pin": pin, "frc_season": "2027"})
	if sc["frc_season"] != "2027" || sc["pin_source"] != "arg" {
		t.Fatalf("override: %v %v", sc["frc_season"], sc["pin_source"])
	}
	res, _, text := call(t, cs, "frc_search", map[string]any{"query": "x", "pin": "pin1.garbage"})
	if !res.IsError || !strings.Contains(text, "frc_context") {
		t.Fatalf("bad pin must be an instructive error: %s", text)
	}
}

func TestContextNoFilesystemOnHostedServer(t *testing.T) {
	srv := mcpserver.New(testfixture.Engine(t), mcpserver.Options{Version: "t", NoFilesystem: true})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, _ := srv.MCP().Connect(ctx, st, nil)
	cs, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	defer cs.Close()
	defer ss.Close()
	res, _, text := call(t, cs, "frc_context", map[string]any{"project_root": "/etc"})
	if !res.IsError || !strings.Contains(text, "disabled") {
		t.Fatalf("project_root must be refused on hosted servers: %s", text)
	}
	_, sc, _ := call(t, cs, "frc_context", map[string]any{"declare": map[string]any{"frc_season": "2027", "language": "java"}})
	if sc["frc_season"] != "2027" || sc["pin"] == "" {
		t.Fatalf("declare: %v", sc)
	}
}

func BenchmarkSearchOverProtocol(b *testing.B) {
	srv := mcpserver.New(testfixture.Engine(b), mcpserver.Options{Version: "bench"})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, _ := srv.MCP().Connect(ctx, st, nil)
	cs, _ := mcp.NewClient(&mcp.Implementation{Name: "b", Version: "0"}, nil).Connect(ctx, ct, nil)
	defer cs.Close()
	defer ss.Close()
	params := &mcp.CallToolParams{Name: "frc_search", Arguments: map[string]any{"query": "How do I configure Motion Magic on a TalonFX?", "language": "java"}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := cs.CallTool(ctx, params); err != nil {
			b.Fatal(err)
		}
	}
}

// catalogEngine builds a shard holding only vendordep facts.
func catalogEngine(t *testing.T) *retrieve.Engine {
	t.Helper()
	ctx := context.Background()
	p := filepath.Join(t.TempDir(), "vendordeps-2026.sqlite")
	w, err := index.Create(ctx, p, "vendordeps-2026")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1_790_000_000, 0)
	for _, v := range []index.Vendordep{
		{UUID: "3f48eb8c", Name: "REVLib", Version: "2026.0.0", Season: "2026", Channel: "stable", FRCYear: "2026", FileName: "REVLib.json",
			JSONURL: "https://software-metadata.revrobotics.com/REVLib-2026.json"},
		{UUID: "3f48eb8c", Name: "REVLib", Version: "2026.0.5", Season: "2026", Channel: "stable", FRCYear: "2026", FileName: "REVLib.json",
			JSONURL: "https://software-metadata.revrobotics.com/REVLib-2026.json"},
		{UUID: "badjson", Name: "Sketchy", Version: "1.0.0", Season: "2026", Channel: "stable", FRCYear: "2026", JSONURL: "http://evil.example/x.json"},
	} {
		v.Raw, v.SourceURL, v.UpstreamRev, v.RetrievedAt = []byte(`{}`), "https://raw.githubusercontent.com/x", "r", now
		if err := w.AddVendordep(ctx, v); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		t.Fatal(err)
	}
	r, err := index.Open(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return retrieve.New([]*index.Reader{r}, retrieve.Options{DefaultSeason: "2026"})
}

func TestVendordepTool(t *testing.T) {
	cs := connect(t, catalogEngine(t))
	_, sc, text := call(t, cs, "frc_vendordep", map[string]any{"name": "sparkmax"})
	lib, _ := sc["library"].(map[string]any)
	if lib["latest_version"] != "2026.0.5" || lib["install"] != "./gradlew vendordep --url=https://software-metadata.revrobotics.com/REVLib-2026.json" {
		t.Fatalf("resolve: %v\n%s", sc, text)
	}
	_, sc, _ = call(t, cs, "frc_vendordep", map[string]any{"vendordeps": []string{"REVLib@2026.0.0"}})
	f := sc["findings"].([]any)[0].(map[string]any)
	if f["status"] != "outdated" || f["latest"] != "2026.0.5" {
		t.Fatalf("set mode: %v", f)
	}
	// A non-https jsonUrl from the catalog is never handed out as an install command.
	_, sc, _ = call(t, cs, "frc_vendordep", map[string]any{"name": "Sketchy"})
	if lib := sc["library"].(map[string]any); lib["install"] != nil || lib["json_url"] != nil {
		t.Fatalf("non-https install URL leaked: %v", lib)
	}
	_, sc, _ = call(t, cs, "frc_vendordep", map[string]any{"name": "nonexistentlib"})
	if sc["status"] != "no_match" || len(sc["candidates"].([]any)) == 0 {
		t.Fatalf("unknown: %v", sc)
	}
}

func TestVerifyCodeTool(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	src := "package frc.robot;\nimport edu.wpi.first.math.kinematics.SwerveDriveKinematics;\npublic class Drive {}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "Drive.java"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(t.TempDir(), "Secret.java")
	if err := os.WriteFile(secret, []byte("class S {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := mcpserver.New(testfixture.Engine(t), mcpserver.Options{Version: "t", ProjectRoot: root})
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	ss, _ := srv.MCP().Connect(ctx, st, nil)
	cs, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	defer cs.Close()
	defer ss.Close()

	_, sc, text := call(t, cs, "frc_verify_code", map[string]any{"path": "src/Drive.java", "frc_season": "2027"})
	if sc["errors"] != float64(1) || sc["status"] != "version_mismatch" || !strings.Contains(text, "use org.wpilib.math.kinematics.SwerveDriveKinematics") {
		t.Fatalf("verify path: %v\n%s", sc, text)
	}
	_, sc, _ = call(t, cs, "frc_verify_code", map[string]any{"code": src, "frc_season": "2026"})
	if sc["errors"] != float64(0) || sc["status"] != "ok" {
		t.Fatalf("2026 clean: %v", sc)
	}
	for _, bad := range []string{"../" + filepath.Base(filepath.Dir(secret)) + "/Secret.java", secret, "src/../../etc/passwd.java", "build.gradle"} {
		res, _, text := call(t, cs, "frc_verify_code", map[string]any{"path": bad})
		if !res.IsError {
			t.Errorf("path %q must be refused: %s", bad, text)
		}
	}
	hosted := mcpserver.New(testfixture.Engine(t), mcpserver.Options{Version: "t", ProjectRoot: root, NoFilesystem: true})
	ct2, st2 := mcp.NewInMemoryTransports()
	ss2, _ := hosted.MCP().Connect(ctx, st2, nil)
	cs2, _ := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct2, nil)
	defer cs2.Close()
	defer ss2.Close()
	if res, _, _ := call(t, cs2, "frc_verify_code", map[string]any{"path": "src/Drive.java"}); !res.IsError {
		t.Error("hosted server must refuse path")
	}
}
