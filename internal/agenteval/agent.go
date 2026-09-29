package agenteval

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Conditions of the A/B.
const (
	WithMCP  = "mcp"
	Baseline = "baseline"
)

// Agent runs a coding agent headless in a workspace.
type Agent struct {
	// Bin is the claude CLI; Model its --model value.
	Bin, Model string
	// FRCMCP is the frc-mcp binary and Index the shard directory it serves
	// (only for the mcp condition).
	FRCMCP, Index string
	Timeout       time.Duration
}

// Outcome is what one agent run did.
type Outcome struct {
	ToolCalls  map[string]int `json:"tool_calls"`
	FRCCalls   int            `json:"frc_calls"`
	MCPServers []string       `json:"mcp_servers"`     // from the session's init message: proves the condition
	Model      string         `json:"model,omitempty"` // resolved model id from the init message
	Turns      int            `json:"turns"`
	CostUSD    float64        `json:"cost_usd"`
	InputTok   int            `json:"input_tokens"`
	OutputTok  int            `json:"output_tokens"`
	Millis     int64          `json:"ms"`
	IsError    bool           `json:"is_error,omitempty"`
	Error      string         `json:"error,omitempty"`
	Final      string         `json:"final,omitempty"`
}

// Suffix is appended to every task prompt, in both conditions.
const Suffix = "\n\nWork only inside the current directory, which is the robot project. Create or edit the files with your file tools; " +
	"do not just describe the code. You cannot run Gradle or any shell command here."

// allowed are the only tools the agent may use (no shell, no web): the A/B
// measures API knowledge, and the workspace is disposable.
var allowed = []string{"Read", "Write", "Edit", "Glob", "Grep"}

// Run executes the agent once. Transcript lines are appended to transcript
// (stream-json) when non-nil.
func (a Agent) Run(ctx context.Context, dir, prompt, condition string, transcript *os.File) (Outcome, error) {
	out := Outcome{ToolCalls: map[string]int{}}
	servers := map[string]any{}
	tools := append([]string{}, allowed...)
	if condition == WithMCP {
		servers["frc"] = map[string]any{"command": a.FRCMCP, "args": []string{"serve", "--offline", "--index", a.Index}}
		tools = append(tools, "mcp__frc")
	}
	cfg, err := json.Marshal(map[string]any{"mcpServers": servers})
	if err != nil {
		return out, err
	}
	cfgPath := filepath.Join(filepath.Dir(dir), "mcp.json")
	if err := os.WriteFile(cfgPath, cfg, 0o600); err != nil {
		return out, err
	}
	args := []string{"-p", prompt + Suffix, "--output-format", "stream-json", "--verbose",
		"--model", a.Model, "--permission-mode", "dontAsk", "--allowedTools", strings.Join(tools, ","),
		"--disallowedTools", "Bash,WebFetch,WebSearch,Task,Agent,NotebookEdit",
		"--mcp-config", cfgPath, "--strict-mcp-config", "--setting-sources", "project", "--no-session-persistence"}
	timeout := a.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Minute
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, a.Bin, args...) //nolint:gosec // G204: the operator's agent CLI with fixed flags
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1", "DISABLE_AUTOUPDATER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return out, err
	}
	t0 := time.Now()
	if err := cmd.Start(); err != nil {
		return out, fmt.Errorf("agenteval: start %s: %w", a.Bin, err)
	}
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 1<<20), 64<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if transcript != nil {
			_, _ = transcript.Write(append(append([]byte{}, line...), '\n'))
		}
		parseEvent(line, &out)
	}
	werr := cmd.Wait()
	out.Millis = time.Since(t0).Milliseconds()
	if werr != nil && out.Final == "" {
		out.IsError = true
		out.Error = strings.TrimSpace(werr.Error() + " " + tail(stderr.String(), 600))
	}
	return out, nil
}

func parseEvent(line []byte, out *Outcome) {
	var ev struct {
		Type       string `json:"type"`
		Subtype    string `json:"subtype"`
		Model      string `json:"model"`
		MCPServers []struct {
			Name, Status string
		} `json:"mcp_servers"`
		Message struct {
			Content []struct {
				Type string `json:"type"`
				Name string `json:"name"`
			} `json:"content"`
		} `json:"message"`
		NumTurns int     `json:"num_turns"`
		Cost     float64 `json:"total_cost_usd"`
		IsError  bool    `json:"is_error"`
		Result   string  `json:"result"`
		Usage    struct {
			Input       int `json:"input_tokens"`
			CacheRead   int `json:"cache_read_input_tokens"`
			CacheCreate int `json:"cache_creation_input_tokens"`
			Output      int `json:"output_tokens"`
		} `json:"usage"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return
	}
	switch ev.Type {
	case "system":
		if ev.Subtype == "init" {
			out.Model = ev.Model
			for _, s := range ev.MCPServers {
				out.MCPServers = append(out.MCPServers, s.Name+":"+s.Status)
			}
			sort.Strings(out.MCPServers)
		}
	case "assistant":
		for _, c := range ev.Message.Content {
			if c.Type == "tool_use" {
				out.ToolCalls[c.Name]++
				if strings.HasPrefix(c.Name, "mcp__frc__") {
					out.FRCCalls++
				}
			}
		}
	case "result":
		out.Turns, out.CostUSD, out.IsError = ev.NumTurns, ev.Cost, ev.IsError
		out.InputTok = ev.Usage.Input + ev.Usage.CacheRead + ev.Usage.CacheCreate
		out.OutputTok = ev.Usage.Output
		out.Final = tail(ev.Result, 1500)
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n:]
}
