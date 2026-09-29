package main_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/testfixture"
)

// TestStdioEndToEnd builds the real binary, launches it over stdio exactly as
// an MCP client would, and checks cold start and a search round trip.
func TestStdioEndToEnd(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	tmp := t.TempDir()
	bin := filepath.Join(tmp, "frc-mcp")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.CommandContext(ctx, "go", "build", "-o", bin, ".")
	build.Env = append(build.Environ(), "CGO_ENABLED=0")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	shards := filepath.Join(tmp, "shards")
	root := testfixture.Root()
	if _, err := index.BuildFromJSONL(ctx, filepath.Join(shards, "fixture.sqlite"), "fixture",
		filepath.Join(root, "chunks.jsonl"), filepath.Join(root, "symbols.jsonl")); err != nil {
		t.Fatal(err)
	}

	client := mcp.NewClient(&mcp.Implementation{Name: "e2e", Version: "0"}, nil)
	launch := func() (*mcp.ClientSession, *mcp.ListToolsResult, time.Duration) {
		start := time.Now()
		cs, err := client.Connect(ctx, &mcp.CommandTransport{Command: exec.Command(bin, "serve", "--index", shards)}, nil)
		if err != nil {
			t.Fatal(err)
		}
		tools, err := cs.ListTools(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		return cs, tools, time.Since(start)
	}
	// The first exec of a freshly built binary includes OS-level scanning
	// (e.g. macOS Gatekeeper/XProtect); the budget applies to warm launches.
	first, _, firstLat := launch()
	first.Close()
	var cold time.Duration
	for range 3 {
		c, _, d := launch()
		c.Close()
		cold = max(cold, d)
	}
	cs, tools, _ := launch()
	var err error
	defer cs.Close()
	t.Logf("first exec → tools/list: %s; subsequent launches (max of 3): %s", firstLat, cold)
	if len(tools.Tools) != 7 {
		t.Fatalf("tools = %d", len(tools.Tools))
	}
	if cold > time.Second { // generous for CI runners; laptop budget is 150 ms
		t.Errorf("cold start %s", cold)
	}
	// The index loads in the background; poll until it is ready.
	var res *mcp.CallToolResult
	for deadline := time.Now().Add(10 * time.Second); ; {
		res, err = cs.CallTool(ctx, &mcp.CallToolParams{Name: "frc_search",
			Arguments: map[string]any{"query": "SwerveDriveKinematics", "frc_season": "2027"}})
		if err != nil || res.IsError {
			t.Fatalf("call: %v %+v", err, res)
		}
		if !strings.Contains(res.Content[0].(*mcp.TextContent).Text, "status: syncing") || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	text := res.Content[0].(*mcp.TextContent).Text
	if !strings.Contains(text, "org.wpilib.math.kinematics") || strings.Contains(text, "season 2026") {
		t.Fatalf("2027 pin not honored:\n%s", text)
	}
}
