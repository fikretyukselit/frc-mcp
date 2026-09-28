package m2v

import (
	"encoding/json"
	"math"
	"os"
	"testing"
)

// modelDir locates the potion model; tests skip when it is absent (it is a
// 32 MB download fetched by `make model`, not committed).
func modelDir(tb testing.TB) string {
	d := os.Getenv("FRC_MCP_MODEL_DIR")
	if d == "" {
		d = "../../../.shards/models/potion-code-16M-v2"
	}
	if _, err := os.Stat(d + "/model.safetensors"); err != nil {
		tb.Skipf("model not found at %s (run `make model`)", d)
	}
	return d
}

type golden struct {
	Text   string    `json:"text"`
	Tokens []string  `json:"tokens"`
	IDs    []int32   `json:"ids"`
	Vec    []float64 `json:"vec"`
}

// TestMatchesPythonReference compares token ids and vectors with output of
// the official model2vec package (testdata/m2v_golden.json).
func TestMatchesPythonReference(t *testing.T) {
	m, err := Load(modelDir(t), "potion-code-16M-v2")
	if err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/m2v_golden.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []golden
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		var want []int32
		for _, id := range c.IDs {
			if id != m.unkID {
				want = append(want, id)
			}
		}
		got := m.Tokenize(c.Text, nil)
		if len(got) != len(want) {
			t.Errorf("%q: %d ids, want %d\n got %v\nwant %v (%v)", c.Text, len(got), len(want), got, want, c.Tokens)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%q: id[%d]=%d want %d (%v)", c.Text, i, got[i], want[i], c.Tokens)
				break
			}
		}
		v := m.Encode(c.Text, nil)
		var dot, na, nb float64
		for i := range v {
			dot += float64(v[i]) * c.Vec[i]
			na += float64(v[i]) * float64(v[i])
			nb += c.Vec[i] * c.Vec[i]
		}
		if nb == 0 {
			if na != 0 {
				t.Errorf("%q: want zero vector", c.Text)
			}
			continue
		}
		if cos := dot / math.Sqrt(na*nb); cos < 0.9999 {
			t.Errorf("%q: cosine vs reference = %.6f", c.Text, cos)
		}
	}
}

func TestF16(t *testing.T) {
	for h, want := range map[uint16]float32{0x3c00: 1, 0xc000: -2, 0x0000: 0, 0x3555: 0.33325195, 0x0001: 5.9604645e-08, 0x7bff: 65504} {
		if got := f16(h); got != want {
			t.Errorf("f16(%#x) = %v, want %v", h, got, want)
		}
	}
}

func BenchmarkEncode(b *testing.B) {
	m, err := Load(modelDir(b), "potion-code-16M-v2")
	if err != nil {
		b.Fatal(err)
	}
	dst := make([]float32, m.Dims)
	b.ReportAllocs()
	for b.Loop() {
		dst = m.Encode("How do I configure Motion Magic on a TalonFX with Phoenix 6?", dst)
	}
}
