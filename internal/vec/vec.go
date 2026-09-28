// Package vec implements the dense-retrieval layer: int8-quantized,
// L2-normalized vectors in a row-aligned, memory-mapped file, searched by an
// exact flat scan (docs/retrieval.md §4, ADR-0002).
//
// At frc-mcp's scale (≤ ~150k chunks × 256 dims) an exact scan over int8 data
// is a few milliseconds on one core, needs no index build, has perfect recall,
// and keeps the layer trivially compatible across profiles (ADR-0005).
package vec

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"runtime"
	"sync"
	"unsafe"
)

// File layout (little-endian):
//
//	[0:8)    magic "FRCVEC01"
//	[8:12)   dims   uint32
//	[12:16)  rows   uint32
//	[16:18)  len(model id) uint16, followed by the model id bytes
//	…        zero padding to a 64-byte boundary
//	scales   rows × float32   (per-row dequantization scale)
//	…        zero padding to a 64-byte boundary
//	data     rows × dims × int8
const magic = "FRCVEC01"

// ParallelThreshold is the row count above which the exact scan fans out
// across GOMAXPROCS workers. Below it the goroutine overhead outweighs the gain.
const ParallelThreshold = 32 << 10

// Layer is an open, immutable vector layer. It is safe for concurrent use.
type Layer struct {
	ModelID string
	Dims    int
	Rows    int
	scales  []float32
	data    []int8
	unmap   func() error
}

// Hit is a scored row. Scores are cosine similarities in [-1, 1].
type Hit struct {
	Row   uint32
	Score float32
}

// Quantize converts an L2-normalized float vector to int8 with a symmetric
// per-vector scale (max |x| → 127). dst is reused when large enough.
func Quantize(v []float32, dst []int8) ([]int8, float32) {
	if cap(dst) < len(v) {
		dst = make([]int8, len(v))
	}
	dst = dst[:len(v)]
	var m float32
	for _, x := range v {
		if a := float32(math.Abs(float64(x))); a > m {
			m = a
		}
	}
	if m == 0 {
		clear(dst)
		return dst, 0
	}
	s := 127 / m
	for i, x := range v {
		dst[i] = int8(math.Round(float64(x * s)))
	}
	return dst, m / 127
}

// Write serializes a layer. vectors must all have len == dims and be
// L2-normalized; row i corresponds to chunk vec_row i.
func Write(path, modelID string, dims int, vectors [][]float32) error {
	if dims <= 0 || dims > 1<<16 || len(modelID) > 1<<15 {
		return fmt.Errorf("vec: invalid dims %d or model id", dims)
	}
	hdrLen := 18 + len(modelID)
	scalesOff := hdrLen + pad64(hdrLen)
	dataOff := scalesOff + 4*len(vectors) + pad64(scalesOff+4*len(vectors))
	buf := make([]byte, dataOff+len(vectors)*dims)
	copy(buf, magic)
	binary.LittleEndian.PutUint32(buf[8:], uint32(dims))
	binary.LittleEndian.PutUint32(buf[12:], uint32(len(vectors)))
	binary.LittleEndian.PutUint16(buf[16:], uint16(len(modelID)))
	copy(buf[18:], modelID)
	q := make([]int8, dims)
	for i, v := range vectors {
		if len(v) != dims {
			return fmt.Errorf("vec: row %d has %d dims, want %d", i, len(v), dims)
		}
		var s float32
		q, s = Quantize(v, q)
		binary.LittleEndian.PutUint32(buf[scalesOff+4*i:], math.Float32bits(s))
		row := buf[dataOff+i*dims : dataOff+(i+1)*dims]
		for j, x := range q {
			row[j] = byte(x)
		}
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Open memory-maps a layer (read-only).
func Open(path string) (*Layer, error) {
	b, unmap, err := mapFile(path)
	if err != nil {
		return nil, err
	}
	l, err := parse(b)
	if err != nil {
		_ = unmap()
		return nil, fmt.Errorf("vec: %s: %w", path, err)
	}
	l.unmap = unmap
	return l, nil
}

// Close unmaps the layer. The Layer must not be used afterwards.
func (l *Layer) Close() error {
	if l.unmap == nil {
		return nil
	}
	err := l.unmap()
	l.unmap, l.data, l.scales = nil, nil, nil
	return err
}

func parse(b []byte) (*Layer, error) {
	if len(b) < 18 || string(b[:8]) != magic {
		return nil, errors.New("bad magic")
	}
	dims := int(binary.LittleEndian.Uint32(b[8:]))
	rows := int(binary.LittleEndian.Uint32(b[12:]))
	idLen := int(binary.LittleEndian.Uint16(b[16:]))
	off := 18 + idLen
	if dims <= 0 || off > len(b) {
		return nil, errors.New("corrupt header")
	}
	l := &Layer{ModelID: string(b[18:off]), Dims: dims, Rows: rows}
	off += pad64(off)
	scalesEnd := off + 4*rows
	dataOff := scalesEnd + pad64(scalesEnd)
	if dataOff+rows*dims != len(b) {
		return nil, fmt.Errorf("size mismatch: header says %d×%d, file has %d bytes", rows, dims, len(b))
	}
	if rows > 0 {
		// The mmapped file is page-aligned and offsets are 64-byte padded, so
		// reinterpreting the bytes as float32/int8 slices is safe and zero-copy.
		if uintptr(unsafe.Pointer(&b[off]))%4 != 0 { //nolint:gosec // G103: audited, alignment checked
			return nil, errors.New("unaligned scales")
		}
		l.scales = unsafe.Slice((*float32)(unsafe.Pointer(&b[off])), rows)     //nolint:gosec // G103: audited above
		l.data = unsafe.Slice((*int8)(unsafe.Pointer(&b[dataOff])), rows*dims) //nolint:gosec // G103: audited above
	}
	return l, nil
}

func pad64(n int) int { return (64 - n%64) % 64 }

// Search returns the k best rows for an int8 query (from Quantize), in
// descending score order. allow, when non-nil, is a bitset of permitted rows
// (metadata pre-filter). dst is reused.
func (l *Layer) Search(q []int8, qscale float32, allow []uint64, k int, dst []Hit) []Hit {
	dst = dst[:0]
	if k <= 0 || l.Rows == 0 || len(q) != l.Dims {
		return dst
	}
	wq := widen(q)
	defer widePool.Put(wq)
	workers := 1
	if l.Rows >= ParallelThreshold {
		workers = min(runtime.GOMAXPROCS(0), l.Rows/(ParallelThreshold/4))
	}
	if workers <= 1 {
		h := newHeap(k)
		l.scan(*wq, qscale, allow, 0, l.Rows, h)
		return h.sorted(dst)
	}
	return l.parallel(workers, k, dst, func(lo, hi int, h *topK) { l.scan(*wq, qscale, allow, lo, hi, h) })
}

func (l *Layer) parallel(workers, k int, dst []Hit, fn func(lo, hi int, h *topK)) []Hit {
	heaps := make([]*topK, workers)
	var wg sync.WaitGroup
	step := (l.Rows + workers - 1) / workers
	for w := range workers {
		lo, hi := w*step, min((w+1)*step, l.Rows)
		heaps[w] = newHeap(k)
		wg.Go(func() { fn(lo, hi, heaps[w]) })
	}
	wg.Wait()
	merged := heaps[0]
	for _, h := range heaps[1:] {
		for _, x := range h.items {
			merged.push(x)
		}
		h.items = h.items[:0]
		heapPool.Put(h)
	}
	return merged.sorted(dst)
}

func (l *Layer) scan(q []int32, qscale float32, allow []uint64, lo, hi int, h *topK) {
	d := l.Dims
	for r := lo; r < hi; r++ {
		if allow != nil && allow[r>>6]&(1<<(uint(r)&63)) == 0 {
			continue
		}
		s := float32(DotWide(q, l.data[r*d:r*d+d])) * qscale * l.scales[r]
		h.push(Hit{Row: uint32(r), Score: s})
	}
}

var widePool = sync.Pool{New: func() any { return new([]int32) }}

func widen(q []int8) *[]int32 {
	p := widePool.Get().(*[]int32)
	if cap(*p) < len(q) {
		*p = make([]int32, len(q))
	}
	*p = (*p)[:len(q)]
	for i, x := range q {
		(*p)[i] = int32(x)
	}
	return p
}

// DotWide is the hot-path inner product: the query is pre-widened to int32
// once per search, so each step is a single sign-extending load and a
// multiply-add. Unrolled by 16 into 4 independent accumulators (≈28% faster
// than the int8×int8 form, measured on arm64).
//
// A sign-bit Hamming prefilter with int8 rescoring was evaluated and rejected:
// at 100k×256 it saved only ~18% latency while dropping recall@10 to 0.60.
func DotWide(a []int32, b []int8) int32 {
	n := len(a)
	b = b[:n]
	var s0, s1, s2, s3 int32
	i := 0
	for ; i+16 <= n; i += 16 {
		aa, bb := a[i:i+16:i+16], b[i:i+16:i+16]
		s0 += aa[0]*int32(bb[0]) + aa[4]*int32(bb[4]) + aa[8]*int32(bb[8]) + aa[12]*int32(bb[12])
		s1 += aa[1]*int32(bb[1]) + aa[5]*int32(bb[5]) + aa[9]*int32(bb[9]) + aa[13]*int32(bb[13])
		s2 += aa[2]*int32(bb[2]) + aa[6]*int32(bb[6]) + aa[10]*int32(bb[10]) + aa[14]*int32(bb[14])
		s3 += aa[3]*int32(bb[3]) + aa[7]*int32(bb[7]) + aa[11]*int32(bb[11]) + aa[15]*int32(bb[15])
	}
	for ; i < n; i++ {
		s0 += a[i] * int32(b[i])
	}
	return s0 + s1 + s2 + s3
}

// Dot is the int8 inner product with int32 accumulation, unrolled by 8 with
// independent accumulators to break the dependency chain. len(a) must equal
// len(b); the reslice lets the compiler drop bounds checks in the loop.
func Dot(a, b []int8) int32 {
	n := len(a)
	b = b[:n]
	var s0, s1, s2, s3, s4, s5, s6, s7 int32
	i := 0
	for ; i+8 <= n; i += 8 {
		aa, bb := a[i:i+8:i+8], b[i:i+8:i+8]
		s0 += int32(aa[0]) * int32(bb[0])
		s1 += int32(aa[1]) * int32(bb[1])
		s2 += int32(aa[2]) * int32(bb[2])
		s3 += int32(aa[3]) * int32(bb[3])
		s4 += int32(aa[4]) * int32(bb[4])
		s5 += int32(aa[5]) * int32(bb[5])
		s6 += int32(aa[6]) * int32(bb[6])
		s7 += int32(aa[7]) * int32(bb[7])
	}
	for ; i < n; i++ {
		s0 += int32(a[i]) * int32(b[i])
	}
	return s0 + s1 + s2 + s3 + s4 + s5 + s6 + s7
}

// topK is a fixed-capacity min-heap on Score (root = current k-th best), so
// each candidate costs one comparison unless it enters the top k.
type topK struct {
	items []Hit
	k     int
}

var heapPool = sync.Pool{New: func() any { return &topK{} }}

func newHeap(k int) *topK {
	h := heapPool.Get().(*topK)
	if cap(h.items) < k {
		h.items = make([]Hit, 0, k)
	}
	h.items, h.k = h.items[:0], k
	return h
}

func (h *topK) push(x Hit) {
	if len(h.items) < h.k {
		h.items = append(h.items, x)
		h.up(len(h.items) - 1)
		return
	}
	if !less(h.items[0], x) {
		return
	}
	h.items[0] = x
	h.down(0)
}

// less orders by score, then by row (lower row wins ties → deterministic).
func less(a, b Hit) bool {
	if a.Score != b.Score {
		return a.Score < b.Score
	}
	return a.Row > b.Row
}

func (h *topK) up(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if !less(h.items[i], h.items[p]) {
			return
		}
		h.items[i], h.items[p] = h.items[p], h.items[i]
		i = p
	}
}

func (h *topK) down(i int) {
	n := len(h.items)
	for {
		l := 2*i + 1
		if l >= n {
			return
		}
		m := l
		if r := l + 1; r < n && less(h.items[r], h.items[l]) {
			m = r
		}
		if !less(h.items[m], h.items[i]) {
			return
		}
		h.items[i], h.items[m] = h.items[m], h.items[i]
		i = m
	}
}

// sorted drains the heap into dst in descending order and recycles the heap.
func (h *topK) sorted(dst []Hit) []Hit {
	n := len(h.items)
	if cap(dst) < n {
		dst = make([]Hit, n)
	}
	dst = dst[:n]
	for i := n - 1; i >= 0; i-- {
		dst[i] = h.items[0]
		last := len(h.items) - 1
		h.items[0] = h.items[last]
		h.items = h.items[:last]
		h.down(0)
	}
	heapPool.Put(h)
	return dst
}
