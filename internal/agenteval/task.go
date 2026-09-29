package agenteval

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

// Task is one agent coding task (eval/tasks/*.yaml).
type Task struct {
	ID     string `yaml:"id" json:"id"`
	Season string `yaml:"season" json:"season"`
	// Vendordeps are catalog names (REVLib, "CTRE-Phoenix (v6)", …) whose
	// JSON is placed in vendordeps/ and whose Java jars join the classpath.
	Vendordeps []string `yaml:"vendordeps" json:"vendordeps,omitempty"`
	// Prompt is what a student would ask. It names the season and library
	// versions (as the project's build files do) but never the API names
	// being tested.
	Prompt string `yaml:"prompt" json:"prompt"`
	// Files the agent must create (compiled together with anything else it
	// writes under src/main/java).
	Files []string `yaml:"files" json:"files"`
	Tags  []string `yaml:"tags" json:"tags,omitempty"`
}

var taskIDRe = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)

// ErrTasks is returned for invalid task files.
var ErrTasks = errors.New("agenteval: invalid tasks")

// LoadTasks reads every *.yaml in dir (each a list of tasks).
func LoadTasks(dir string) ([]Task, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.yaml"))
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	seasons := Seasons()
	seen := map[string]bool{}
	var out []Task
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var ts []Task
		dec := yaml.NewDecoder(strings.NewReader(string(b)))
		dec.KnownFields(true)
		if err := dec.Decode(&ts); err != nil {
			return nil, fmt.Errorf("%w: %s: %w", ErrTasks, filepath.Base(p), err)
		}
		for _, t := range ts {
			switch {
			case !taskIDRe.MatchString(t.ID) || seen[t.ID]:
				return nil, fmt.Errorf("%w: %s: id %q must be unique kebab-case", ErrTasks, filepath.Base(p), t.ID)
			case seasons[t.Season].Season == "":
				return nil, fmt.Errorf("%w: %s: season %q not supported", ErrTasks, t.ID, t.Season)
			case strings.TrimSpace(t.Prompt) == "" || len(t.Files) == 0:
				return nil, fmt.Errorf("%w: %s: needs prompt and files", ErrTasks, t.ID)
			}
			for _, f := range t.Files {
				if !strings.HasPrefix(f, "src/main/java/") || !strings.HasSuffix(f, ".java") || strings.Contains(f, "..") {
					return nil, fmt.Errorf("%w: %s: file %q must be src/main/java/…/*.java", ErrTasks, t.ID, f)
				}
			}
			seen[t.ID] = true
			t.Prompt = strings.TrimSpace(t.Prompt)
			out = append(out, t)
		}
	}
	return out, nil
}
