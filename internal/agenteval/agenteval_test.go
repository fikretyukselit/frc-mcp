package agenteval

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoTasks(t *testing.T) {
	ts, err := LoadTasks("../../eval/tasks")
	if err != nil {
		t.Fatal(err)
	}
	if len(ts) < 20 {
		t.Fatalf("tasks = %d", len(ts))
	}
	for _, tk := range ts {
		// Prompts must not hand the agent the API under test.
		for _, leak := range []string{"ChassisVelocities", "SwerveModuleVelocity", "Selectable", "Tunables", "Telemetry.log",
			"CommandNiDsXboxController", "LoggedNetworkChooser", "setThrottle", "DrivetrainSplineTrajectoryGenerator"} {
			if strings.Contains(tk.Prompt, leak) {
				t.Errorf("%s: prompt names %s", tk.ID, leak)
			}
		}
	}
}

func TestLoadTasksRejects(t *testing.T) {
	for name, doc := range map[string]string{
		"bad season": "- {id: a, season: \"2019\", prompt: x, files: [src/main/java/A.java]}\n",
		"bad file":   "- {id: a, season: \"2027\", prompt: x, files: [../A.java]}\n",
		"no files":   "- {id: a, season: \"2027\", prompt: x}\n",
		"dup id":     "- {id: a, season: \"2027\", prompt: x, files: [src/main/java/A.java]}\n- {id: a, season: \"2027\", prompt: y, files: [src/main/java/B.java]}\n",
		"unknown":    "- {id: a, season: \"2027\", prompt: x, files: [src/main/java/A.java], extra: 1}\n",
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "t.yaml"), []byte(doc), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := LoadTasks(dir); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func TestArtifactsAndVendordep(t *testing.T) {
	s := Seasons()["2027"]
	var epilogue, wpilibj bool
	for _, a := range append(s.Artifacts, s.Processors...) {
		switch a.Name {
		case "epilogue-processor-java":
			epilogue = a.Group == "org.wpilib.epilogue"
		case "wpilibj-java":
			wpilibj = a.URL() == "https://frcmaven.wpi.edu/artifactory/release/org/wpilib/wpilibj/wpilibj-java/2027.0.0-alpha-7/wpilibj-java-2027.0.0-alpha-7.jar"
		}
	}
	if !epilogue || !wpilibj {
		t.Errorf("artifact coordinates: epilogue=%v wpilibj=%v", epilogue, wpilibj)
	}
	arts, err := VendordepArtifacts([]byte(`{"mavenUrls":["https://maven.revrobotics.com/"],"javaDependencies":[{"groupId":"com.revrobotics.frc","artifactId":"REVLib-java","version":"2027.0.0-alpha-7"}]}`))
	if err != nil || len(arts) != 1 || arts[0].URL() != "https://maven.revrobotics.com/com/revrobotics/frc/REVLib-java/2027.0.0-alpha-7/REVLib-java-2027.0.0-alpha-7.jar" {
		t.Fatalf("%v %v", arts, err)
	}
	f := &Fetcher{}
	f.AllowHost("maven.revrobotics.com")
	for u, want := range map[string]bool{"https://maven.revrobotics.com/x.jar": true, "https://evil.example/x.jar": false, "http://frcmaven.wpi.edu/x": false} {
		if f.allowed(u) != want {
			t.Errorf("allowed(%s) != %v", u, want)
		}
	}
}

func TestParseEvent(t *testing.T) {
	var o Outcome
	o.ToolCalls = map[string]int{}
	for _, l := range []string{
		`{"type":"system","subtype":"init","model":"claude-x","mcp_servers":[{"name":"frc","status":"connected"}]}`,
		`{"type":"assistant","message":{"content":[{"type":"tool_use","name":"mcp__frc__frc_api"},{"type":"text"},{"type":"tool_use","name":"Write"}]}}`,
		`{"type":"result","num_turns":7,"total_cost_usd":0.12,"is_error":false,"result":"done","usage":{"input_tokens":10,"cache_read_input_tokens":90,"output_tokens":5}}`,
		`not json`,
	} {
		parseEvent([]byte(l), &o)
	}
	if o.FRCCalls != 1 || o.ToolCalls["Write"] != 1 || o.Turns != 7 || o.InputTok != 100 || o.MCPServers[0] != "frc:connected" || o.Final != "done" || o.Model != "claude-x" {
		t.Fatalf("%+v", o)
	}
}
