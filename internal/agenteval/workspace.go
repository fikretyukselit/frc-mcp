package agenteval

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Vendordep is a catalog vendordep chosen for a task.
type Vendordep struct {
	Name, FileName string
	Raw            []byte
}

// NewWorkspace creates an empty GradleRIO robot project for a task: the
// build file (so frc_context detects the season), vendordeps/*.json and an
// empty src/main/java/frc/robot. It returns the project directory.
func NewWorkspace(dir string, s Season, deps []Vendordep) (string, error) {
	root := filepath.Join(dir, "robot")
	for _, d := range []string{"vendordeps", "src/main/java/frc/robot", "src/main/deploy"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			return "", err
		}
	}
	gradle := fmt.Sprintf(`plugins {
    id "java"
    id "%s" version "%s"
}

java {
    sourceCompatibility = JavaVersion.VERSION_%d
    targetCompatibility = JavaVersion.VERSION_%d
}

def ROBOT_MAIN_CLASS = "frc.robot.Main"

dependencies {
    annotationProcessor wpi.java.deps.wpilibAnnotations()
    implementation wpi.java.deps.wpilib()
    implementation wpi.java.vendor.java()
}
`, s.PluginID, s.GradleRIO, s.Release, s.Release)
	files := map[string][]byte{
		"build.gradle":                     []byte(gradle),
		"settings.gradle":                  []byte("rootProject.name = \"robot\"\n"),
		"vendordeps/" + commandsFile(s):    s.Commands,
		"src/main/java/frc/robot/.gitkeep": nil,
		".wpilib/wpilib_preferences.json":  []byte(fmt.Sprintf(`{"currentLanguage": "java", "projectYear": "%s", "teamNumber": 9999}`+"\n", s.Season)),
	}
	for _, d := range deps {
		name := d.FileName
		if name == "" {
			name = strings.ReplaceAll(d.Name, " ", "") + ".json"
		}
		files["vendordeps/"+filepath.Base(name)] = d.Raw
	}
	for rel, b := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}

func commandsFile(s Season) string {
	if s.Season >= "2027" {
		return "CommandsV2.json"
	}
	return "WPILibNewCommands.json"
}
