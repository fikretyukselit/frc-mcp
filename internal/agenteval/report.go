package agenteval

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"os"
	"sort"
	"strings"
)

// Result is one (task, condition, trial) run: one JSONL line of a results
// file.
type Result struct {
	Task      string        `json:"task"`
	Season    string        `json:"season"`
	Tags      []string      `json:"tags,omitempty"`
	Condition string        `json:"condition"`
	Trial     int           `json:"trial"`
	Model     string        `json:"model"`
	Agent     Outcome       `json:"agent"`
	Compile   CompileResult `json:"compile"`
	// VerifyErrors is frc_verify_code's error count over the produced
	// files (pinned to the task season): compared with Compile it measures
	// the verifier on real agent code.
	VerifyErrors int    `json:"verify_errors"`
	Dir          string `json:"dir,omitempty"`
	At           string `json:"at"`
}

// Key identifies a run for resuming.
func (r Result) Key() string {
	return fmt.Sprintf("%s|%s|%d|%s", r.Task, r.Condition, r.Trial, r.Model)
}

// ReadResults loads JSONL result files.
func ReadResults(paths ...string) ([]Result, error) {
	var out []Result
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		sc := bufio.NewScanner(f)
		sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
		for sc.Scan() {
			if len(strings.TrimSpace(sc.Text())) == 0 {
				continue
			}
			var r Result
			if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
				f.Close()
				return nil, fmt.Errorf("%s: %w", p, err)
			}
			out = append(out, r)
		}
		f.Close()
		if err := sc.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Arm aggregates one condition.
type Arm struct {
	Runs, Passed   int
	Errors         float64 // mean javac errors
	Cost, Turns    float64 // mean
	FRCCalls       float64 // mean frc_* tool calls
	AgentErrors    int
	VerifyAgrees   int // runs where "verifier errors > 0" == "compile failed"
	VerifyFlagged  int // failed compiles the verifier flagged
	CompileFailed  int
	VerifyFalseErr int // passed compiles the verifier flagged (verifier false positive)
}

// Rate is the compile-pass rate.
func (a Arm) Rate() float64 {
	if a.Runs == 0 {
		return 0
	}
	return float64(a.Passed) / float64(a.Runs)
}

// Summary is a report over results.
type Summary struct {
	Model   string
	Arms    map[string]*Arm // condition → arm
	Seasons map[string]map[string]*Arm
	// Tasks: task → condition → pass rate over trials.
	Tasks map[string]map[string]float64
	// Delta is the mean paired (mcp − baseline) pass-rate difference over
	// tasks, with a 95% bootstrap interval.
	Delta, Lo, Hi float64
	Paired        int
}

// Summarize aggregates results of one model.
func Summarize(rs []Result) Summary {
	s := Summary{Arms: map[string]*Arm{}, Seasons: map[string]map[string]*Arm{}, Tasks: map[string]map[string]float64{}}
	type acc struct{ pass, n int }
	per := map[string]map[string]*acc{}
	for _, r := range rs {
		s.Model = r.Model
		for _, a := range []*Arm{arm(s.Arms, r.Condition), seasonArm(s.Seasons, r.Season, r.Condition)} {
			a.Runs++
			if r.Compile.Pass {
				a.Passed++
			} else {
				a.CompileFailed++
				if r.VerifyErrors > 0 {
					a.VerifyFlagged++
				}
			}
			if r.Compile.Pass && r.VerifyErrors > 0 {
				a.VerifyFalseErr++
			}
			if (r.VerifyErrors > 0) == !r.Compile.Pass {
				a.VerifyAgrees++
			}
			a.Errors += float64(r.Compile.Errors)
			a.Cost += r.Agent.CostUSD
			a.Turns += float64(r.Agent.Turns)
			a.FRCCalls += float64(r.Agent.FRCCalls)
			if r.Agent.IsError {
				a.AgentErrors++
			}
		}
		if per[r.Task] == nil {
			per[r.Task] = map[string]*acc{}
		}
		if per[r.Task][r.Condition] == nil {
			per[r.Task][r.Condition] = &acc{}
		}
		p := per[r.Task][r.Condition]
		p.n++
		if r.Compile.Pass {
			p.pass++
		}
	}
	for _, arms := range append([]map[string]*Arm{s.Arms}, seasonMaps(s.Seasons)...) {
		for _, a := range arms {
			if a.Runs > 0 {
				n := float64(a.Runs)
				a.Errors, a.Cost, a.Turns, a.FRCCalls = a.Errors/n, a.Cost/n, a.Turns/n, a.FRCCalls/n
			}
		}
	}
	var diffs []float64
	for t, conds := range per {
		s.Tasks[t] = map[string]float64{}
		for c, p := range conds {
			s.Tasks[t][c] = float64(p.pass) / float64(p.n)
		}
		m, okM := conds[WithMCP]
		b, okB := conds[Baseline]
		if okM && okB {
			diffs = append(diffs, float64(m.pass)/float64(m.n)-float64(b.pass)/float64(b.n))
		}
	}
	sort.Float64s(diffs) // deterministic bootstrap input order
	s.Paired = len(diffs)
	s.Delta, s.Lo, s.Hi = bootstrap(diffs)
	return s
}

func arm(m map[string]*Arm, c string) *Arm {
	if m[c] == nil {
		m[c] = &Arm{}
	}
	return m[c]
}

func seasonArm(m map[string]map[string]*Arm, season, c string) *Arm {
	if m[season] == nil {
		m[season] = map[string]*Arm{}
	}
	return arm(m[season], c)
}

func seasonMaps(m map[string]map[string]*Arm) []map[string]*Arm {
	out := make([]map[string]*Arm, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	return out
}

// bootstrap returns the mean and a 95% percentile interval (10,000
// resamples, fixed seed so a report is reproducible).
func bootstrap(xs []float64) (mean, lo, hi float64) {
	if len(xs) == 0 {
		return 0, 0, 0
	}
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	rng := rand.New(rand.NewPCG(2027, 4)) //nolint:gosec // G404: a fixed-seed bootstrap must be reproducible, not secret
	const n = 10000
	means := make([]float64, n)
	for i := range means {
		var sum float64
		for range xs {
			sum += xs[rng.IntN(len(xs))]
		}
		means[i] = sum / float64(len(xs))
	}
	sort.Float64s(means)
	return mean, means[n*25/1000], means[n*975/1000]
}

// Markdown renders a summary.
func (s Summary) Markdown() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Model: %s\n\n", s.Model)
	b.WriteString("| Condition | Runs | Compile pass | Mean javac errors | Mean frc_* calls | Mean turns | Mean cost (USD) |\n|---|---|---|---|---|---|---|\n")
	for _, c := range []string{WithMCP, Baseline} {
		if a := s.Arms[c]; a != nil {
			fmt.Fprintf(&b, "| %s | %d | %.1f%% (%d) | %.1f | %.1f | %.1f | %.3f |\n", c, a.Runs, 100*a.Rate(), a.Passed, a.Errors, a.FRCCalls, a.Turns, a.Cost)
		}
	}
	if s.Paired > 0 {
		fmt.Fprintf(&b, "\n**Paired difference (mcp − baseline) over %d tasks: %+.1f pp** (95%% bootstrap interval %+.1f to %+.1f pp)\n",
			s.Paired, 100*s.Delta, 100*s.Lo, 100*s.Hi)
	}
	seasons := make([]string, 0, len(s.Seasons))
	for se := range s.Seasons {
		seasons = append(seasons, se)
	}
	sort.Strings(seasons)
	b.WriteString("\n| Season | mcp pass | baseline pass |\n|---|---|---|\n")
	for _, se := range seasons {
		m, bl := s.Seasons[se][WithMCP], s.Seasons[se][Baseline]
		fmt.Fprintf(&b, "| %s | %s | %s |\n", se, rateCell(m), rateCell(bl))
	}
	tasks := make([]string, 0, len(s.Tasks))
	for t := range s.Tasks {
		tasks = append(tasks, t)
	}
	sort.Strings(tasks)
	b.WriteString("\n| Task | mcp | baseline |\n|---|---|---|\n")
	for _, t := range tasks {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", t, pct(s.Tasks[t], WithMCP), pct(s.Tasks[t], Baseline))
	}
	var fails, flagged, falseErr int
	for _, a := range s.Arms {
		fails += a.CompileFailed
		flagged += a.VerifyFlagged
		falseErr += a.VerifyFalseErr
	}
	fmt.Fprintf(&b, "\nVerifier on agent code: flagged %d of %d failed compiles; %d error(s) on code that compiled.\n", flagged, fails, falseErr)
	return b.String()
}

func rateCell(a *Arm) string {
	if a == nil {
		return "—"
	}
	return fmt.Sprintf("%.1f%% (%d/%d)", 100*a.Rate(), a.Passed, a.Runs)
}

func pct(m map[string]float64, c string) string {
	v, ok := m[c]
	if !ok {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", 100*v)
}
