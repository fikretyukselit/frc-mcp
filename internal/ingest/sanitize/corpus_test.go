package sanitize

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type corpusCase struct {
	ID      string `json:"id"`
	Vector  string `json:"vector"`
	Text    string `json:"text"`
	Want    bool   `json:"want"`
	Payload string `json:"payload"`
}

func readCorpus(t *testing.T, name string) []corpusCase {
	t.Helper()
	fh, err := os.Open("../../../eval/security/injection/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	var out []corpusCase
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var c corpusCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
		out = append(out, c)
	}
	return out
}

// TestInjectionCorpus is the M3 exit gate (docs/security.md §3): suspect
// recall ≥ 0.95 on the injection corpus and no flag on realistic docs text.
func TestInjectionCorpus(t *testing.T) {
	var tp, fn, fp, tn int
	misses := map[string][]string{}
	for _, c := range readCorpus(t, "cases.jsonl") {
		got := Suspect(Clean(c.Text))
		switch {
		case c.Want && got:
			tp++
		case c.Want:
			fn++
			misses[c.Vector] = append(misses[c.Vector], c.ID+": "+c.Text)
		case got:
			fp++
			t.Errorf("false positive %s (%s): %s", c.ID, c.Vector, c.Text)
		default:
			tn++
		}
	}
	recall := float64(tp) / float64(tp+fn)
	t.Logf("suspect recall %.3f (%d/%d), false positives %d/%d", recall, tp, tp+fn, fp, fp+tn)
	for v, ms := range misses {
		for _, m := range ms {
			t.Logf("miss [%s] %s", v, m)
		}
	}
	if recall < 0.95 {
		t.Fatalf("suspect recall %.3f < 0.95", recall)
	}
}

func TestHiddenVectorsRemoved(t *testing.T) {
	for _, c := range readCorpus(t, "hidden.jsonl") {
		if out := Clean(c.Text); strings.Contains(out, c.Payload) {
			t.Errorf("%s (%s): payload survived Clean: %q", c.ID, c.Vector, out)
		}
	}
}

// TestInjectionHoldout reports generalization on cases written after tuning.
// It fails only on false positives (realistic docs must stay unflagged);
// recall is reported, not gated, so the file is never tuned against.
func TestInjectionHoldout(t *testing.T) {
	var tp, fn int
	for _, c := range readCorpus(t, "holdout.jsonl") {
		got := Suspect(Clean(c.Text))
		switch {
		case c.Want && got:
			tp++
		case c.Want:
			fn++
			t.Logf("holdout miss %s: %s", c.ID, c.Text)
		case got:
			t.Errorf("holdout false positive %s: %s", c.ID, c.Text)
		}
	}
	t.Logf("holdout suspect recall %.3f (%d/%d)", float64(tp)/float64(tp+fn), tp, tp+fn)
}
