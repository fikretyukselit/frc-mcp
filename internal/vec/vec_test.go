package vec

import (
	"math"
	"math/rand/v2"
	"path/filepath"
	"sort"
	"testing"
)

func randUnit(r *rand.Rand, d int) []float32 {
	v := make([]float32, d)
	var n float64
	for i := range v {
		x := r.NormFloat64()
		v[i] = float32(x)
		n += x * x
	}
	n = math.Sqrt(n)
	for i := range v {
		v[i] = float32(float64(v[i]) / n)
	}
	return v
}

func corpus(tb testing.TB, rows, dims int) (*Layer, [][]float32) {
	tb.Helper()
	r := rand.New(rand.NewPCG(1, 2))
	vs := make([][]float32, rows)
	for i := range vs {
		vs[i] = randUnit(r, dims)
	}
	p := filepath.Join(tb.TempDir(), "l.vec")
	if err := Write(p, "test-model", dims, vs); err != nil {
		tb.Fatal(err)
	}
	l, err := Open(p)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { l.Close() })
	return l, vs
}

func TestRoundTripAndRecall(t *testing.T) {
	for _, rows := range []int{0, 1, 257, ParallelThreshold + 1000, 100_000} {
		l, vs := corpus(t, rows, 256)
		if l.Rows != rows || l.Dims != 256 || l.ModelID != "test-model" {
			t.Fatalf("header: %+v", l)
		}
		if rows == 0 {
			continue
		}
		r := rand.New(rand.NewPCG(3, 4))
		qf := randUnit(r, 256)
		q, qs := Quantize(qf, nil)
		got := l.Search(q, qs, nil, 10, nil)

		// Exact float ranking as ground truth.
		type sc struct {
			row int
			s   float64
		}
		all := make([]sc, rows)
		for i, v := range vs {
			var s float64
			for j := range v {
				s += float64(v[j]) * float64(qf[j])
			}
			all[i] = sc{i, s}
		}
		sort.Slice(all, func(a, b int) bool { return all[a].s > all[b].s })
		want := map[int]bool{}
		for _, x := range all[:min(10, rows)] {
			want[x.row] = true
		}
		hit := 0
		for i, g := range got {
			if want[int(g.Row)] {
				hit++
			}
			if i > 0 && got[i-1].Score < g.Score {
				t.Fatal("results not sorted")
			}
			if math.Abs(float64(g.Score)-dotF(vs[g.Row], qf)) > 0.02 {
				t.Fatalf("score drift %v vs %v", g.Score, dotF(vs[g.Row], qf))
			}
		}
		if recall := float64(hit) / float64(len(want)); recall < 0.9 {
			t.Fatalf("rows=%d recall@10 = %.2f", rows, recall)
		}
	}
}

func TestAllowBitset(t *testing.T) {
	l, _ := corpus(t, 1000, 64)
	allow := make([]uint64, (1000+63)/64)
	for _, r := range []int{3, 64, 999} {
		allow[r>>6] |= 1 << (uint(r) & 63)
	}
	q, qs := Quantize(randUnit(rand.New(rand.NewPCG(9, 9)), 64), nil)
	got := l.Search(q, qs, allow, 10, nil)
	if len(got) != 3 {
		t.Fatalf("got %d hits, want 3", len(got))
	}
	for _, h := range got {
		if h.Row != 3 && h.Row != 64 && h.Row != 999 {
			t.Fatalf("row %d not allowed", h.Row)
		}
	}
}

func TestOpenRejectsCorrupt(t *testing.T) {
	if _, err := parse([]byte("NOTAVEC!xxxxxxxxxxxxxxxx")); err == nil {
		t.Fatal("want error")
	}
}

func dotF(a, b []float32) float64 {
	var s float64
	for i := range a {
		s += float64(a[i]) * float64(b[i])
	}
	return s
}

func BenchmarkDot256(b *testing.B) {
	r := rand.New(rand.NewPCG(1, 1))
	x, _ := Quantize(randUnit(r, 256), nil)
	y, _ := Quantize(randUnit(r, 256), nil)
	for b.Loop() {
		_ = Dot(x, y)
	}
}

func benchSearch(b *testing.B, rows int) {
	l, _ := corpus(b, rows, 256)
	q, qs := Quantize(randUnit(rand.New(rand.NewPCG(5, 6)), 256), nil)
	var dst []Hit
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		dst = l.Search(q, qs, nil, 50, dst)
	}
}

func BenchmarkSearch10k(b *testing.B)  { benchSearch(b, 10_000) }
func BenchmarkSearch50k(b *testing.B)  { benchSearch(b, 50_000) }
func BenchmarkSearch100k(b *testing.B) { benchSearch(b, 100_000) }

func TestDotWideMatchesDot(t *testing.T) {
	r := rand.New(rand.NewPCG(7, 7))
	for _, n := range []int{1, 15, 16, 17, 256, 383} {
		a, _ := Quantize(randUnit(r, n), nil)
		b, _ := Quantize(randUnit(r, n), nil)
		w := make([]int32, n)
		for i, x := range a {
			w[i] = int32(x)
		}
		if Dot(a, b) != DotWide(w, b) {
			t.Fatalf("n=%d mismatch", n)
		}
	}
}
