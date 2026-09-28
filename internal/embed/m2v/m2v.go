// Package m2v is a pure-Go implementation of model2vec static-embedding
// inference (https://github.com/MinishLab/model2vec) for WordPiece models such
// as minishlab/potion-code-16M-v2.
//
// Encoding = BERT normalization → BERT pre-tokenization → WordPiece →
// drop [UNK] → mean of token embeddings → L2 normalize. There is no neural
// forward pass, so a query embeds in microseconds and no ONNX runtime or cgo
// is needed. Output is golden-tested against the Python reference.
package m2v

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

// Model is a loaded static embedding model. It is safe for concurrent use.
type Model struct {
	ID        string
	Dims      int
	vocab     map[string]int32
	emb       []float32 // rows × Dims, dequantized from float16 at load
	unkID     int32
	normalize bool
	maxChars  int // max_length(512) × median token length, as the reference
	maxTokens int
	maxWord   int
}

type tokenizerJSON struct {
	Normalizer struct {
		Type        string `json:"type"`
		Lowercase   bool   `json:"lowercase"`
		StripAccent *bool  `json:"strip_accents"`
	} `json:"normalizer"`
	PreTokenizer struct {
		Type string `json:"type"`
	} `json:"pre_tokenizer"`
	Model struct {
		Type     string           `json:"type"`
		Unk      string           `json:"unk_token"`
		Prefix   string           `json:"continuing_subword_prefix"`
		MaxChars int              `json:"max_input_chars_per_word"`
		Vocab    map[string]int32 `json:"vocab"`
	} `json:"model"`
}

// Load reads a model directory containing tokenizer.json, model.safetensors
// and config.json (the Hugging Face layout of model2vec models).
func Load(dir, id string) (*Model, error) {
	var tj tokenizerJSON
	if err := readJSON(filepath.Join(dir, "tokenizer.json"), &tj); err != nil {
		return nil, err
	}
	if tj.Model.Type != "WordPiece" || tj.Normalizer.Type != "BertNormalizer" || tj.PreTokenizer.Type != "BertPreTokenizer" {
		return nil, fmt.Errorf("m2v: unsupported tokenizer (%s/%s/%s); only BERT WordPiece models are supported",
			tj.Normalizer.Type, tj.PreTokenizer.Type, tj.Model.Type)
	}
	if !tj.Normalizer.Lowercase || (tj.Normalizer.StripAccent != nil && !*tj.Normalizer.StripAccent) || tj.Model.Prefix != "##" {
		return nil, errors.New("m2v: only uncased WordPiece with '##' prefix is supported")
	}
	var cfg struct {
		Normalize *bool `json:"normalize"`
	}
	_ = readJSON(filepath.Join(dir, "config.json"), &cfg)

	m := &Model{ID: id, vocab: tj.Model.Vocab, normalize: cfg.Normalize == nil || *cfg.Normalize,
		maxTokens: 512, maxWord: tj.Model.MaxChars}
	if m.maxWord == 0 {
		m.maxWord = 100
	}
	unk, ok := m.vocab[tj.Model.Unk]
	if !ok {
		return nil, errors.New("m2v: unk token missing from vocab")
	}
	m.unkID = unk
	// Median token length (in runes) over the vocabulary, as model2vec does.
	lens := make([]int, 0, len(m.vocab))
	for tok := range m.vocab {
		lens = append(lens, utf8.RuneCountInString(tok))
	}
	sort.Ints(lens)
	median := lens[len(lens)/2]
	if len(lens)%2 == 0 {
		median = (lens[len(lens)/2-1] + lens[len(lens)/2]) / 2
	}
	m.maxChars = m.maxTokens * median

	rows, dims, emb, err := loadSafetensorsF16(filepath.Join(dir, "model.safetensors"), "embeddings")
	if err != nil {
		return nil, err
	}
	if rows < len(m.vocab) {
		return nil, fmt.Errorf("m2v: embeddings have %d rows, vocab has %d", rows, len(m.vocab))
	}
	m.Dims, m.emb = dims, emb
	return m, nil
}

var bufPool = sync.Pool{New: func() any { s := make([]int32, 0, 128); return &s }}

// Encode returns the L2-normalized embedding of text into dst (len == Dims),
// allocating when dst is too small. Empty or all-unknown text yields zeros.
func (m *Model) Encode(text string, dst []float32) []float32 {
	if cap(dst) < m.Dims {
		dst = make([]float32, m.Dims)
	}
	dst = dst[:m.Dims]
	clear(dst)
	idsp := bufPool.Get().(*[]int32)
	ids := m.Tokenize(text, (*idsp)[:0])
	if len(ids) > 0 {
		for _, id := range ids {
			row := m.emb[int(id)*m.Dims : int(id+1)*m.Dims]
			for j, x := range row {
				dst[j] += x
			}
		}
		inv := 1 / float32(len(ids))
		var n float64
		for j := range dst {
			dst[j] *= inv
			n += float64(dst[j]) * float64(dst[j])
		}
		if m.normalize {
			s := float32(1 / (math.Sqrt(n) + 1e-32))
			for j := range dst {
				dst[j] *= s
			}
		}
	}
	*idsp = ids
	bufPool.Put(idsp)
	return dst
}

// Tokenize returns WordPiece ids (without [UNK], truncated like the reference).
func (m *Model) Tokenize(text string, dst []int32) []int32 {
	if n := m.maxChars; n > 0 && utf8.RuneCountInString(text) > n {
		i, c := 0, 0
		for i = range text {
			if c == n {
				break
			}
			c++
		}
		text = text[:i]
	}
	for _, word := range preTokenize(normalize(text)) {
		dst = m.wordpiece(word, dst)
		if len(dst) >= m.maxTokens {
			return dst[:m.maxTokens]
		}
	}
	return dst
}

// wordpiece appends ids for one pre-tokenized word (greedy longest match).
func (m *Model) wordpiece(word string, dst []int32) []int32 {
	if utf8.RuneCountInString(word) > m.maxWord {
		return dst // → [UNK], which model2vec drops
	}
	mark := len(dst)
	start := 0
	for start < len(word) {
		end := len(word)
		found := int32(-1)
		for end > start {
			sub := word[start:end]
			if start > 0 {
				sub = "##" + sub
			}
			if id, ok := m.vocab[sub]; ok {
				found = id
				break
			}
			_, size := utf8.DecodeLastRuneInString(word[start:end])
			end -= size
		}
		if found < 0 {
			return dst[:mark] // whole word is [UNK] → dropped
		}
		if found != m.unkID {
			dst = append(dst, found)
		}
		start = end
	}
	return dst
}

// normalize implements HF BertNormalizer(clean_text, handle_chinese_chars,
// lowercase, strip_accents=lowercase).
func normalize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r == 0 || r == 0xFFFD || isControl(r):
			continue
		case isWhitespace(r):
			out = append(out, ' ')
		case isChinese(r):
			out = append(out, ' ', r, ' ')
		default:
			out = append(out, r)
		}
	}
	// strip accents: NFD, drop nonspacing marks; then lowercase.
	d := norm.NFD.String(string(out))
	res := make([]rune, 0, len(d))
	for _, r := range d {
		if unicode.Is(unicode.Mn, r) {
			continue
		}
		res = append(res, unicode.ToLower(r))
	}
	return string(res)
}

// preTokenize implements BertPreTokenizer: split on whitespace, and isolate
// every punctuation character as its own token.
func preTokenize(s string) []string {
	var out []string
	start := -1
	for i, r := range s {
		switch {
		case unicode.IsSpace(r):
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
		case isPunct(r):
			if start >= 0 {
				out = append(out, s[start:i])
				start = -1
			}
			out = append(out, s[i:i+utf8.RuneLen(r)])
		default:
			if start < 0 {
				start = i
			}
		}
	}
	if start >= 0 {
		out = append(out, s[start:])
	}
	return out
}

func isWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || unicode.Is(unicode.Zs, r)
}

func isControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Co, unicode.Cs)
}

func isPunct(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

func isChinese(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3400 && r <= 0x4DBF) || (r >= 0x20000 && r <= 0x2A6DF) ||
		(r >= 0x2A700 && r <= 0x2B73F) || (r >= 0x2B740 && r <= 0x2B81F) || (r >= 0x2B820 && r <= 0x2CEAF) ||
		(r >= 0xF900 && r <= 0xFAFF) || (r >= 0x2F800 && r <= 0x2FA1F)
}

// loadSafetensorsF16 reads one 2-D float16 tensor and dequantizes to float32.
func loadSafetensorsF16(path, name string) (rows, cols int, data []float32, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(b) < 8 {
		return 0, 0, nil, errors.New("m2v: safetensors too short")
	}
	n := binary.LittleEndian.Uint64(b)
	if n > uint64(len(b)-8) || n > 1<<24 {
		return 0, 0, nil, errors.New("m2v: bad safetensors header length")
	}
	var hdr map[string]json.RawMessage
	if err := json.Unmarshal(b[8:8+n], &hdr); err != nil {
		return 0, 0, nil, fmt.Errorf("m2v: safetensors header: %w", err)
	}
	var t struct {
		Dtype   string `json:"dtype"`
		Shape   []int  `json:"shape"`
		Offsets [2]int `json:"data_offsets"`
	}
	raw, ok := hdr[name]
	if !ok {
		return 0, 0, nil, fmt.Errorf("m2v: tensor %q not found", name)
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return 0, 0, nil, err
	}
	if t.Dtype != "F16" || len(t.Shape) != 2 || t.Shape[0] <= 0 || t.Shape[1] <= 0 {
		return 0, 0, nil, fmt.Errorf("m2v: tensor %q must be 2-D F16, got %s %v", name, t.Dtype, t.Shape)
	}
	body := b[8+n:]
	want := t.Shape[0] * t.Shape[1] * 2
	if t.Offsets[0] < 0 || t.Offsets[1] > len(body) || t.Offsets[1]-t.Offsets[0] != want {
		return 0, 0, nil, errors.New("m2v: tensor offsets out of range")
	}
	raw16 := body[t.Offsets[0]:t.Offsets[1]]
	data = make([]float32, t.Shape[0]*t.Shape[1])
	for i := range data {
		data[i] = f16(binary.LittleEndian.Uint16(raw16[2*i:]))
	}
	return t.Shape[0], t.Shape[1], data, nil
}

// f16 converts IEEE 754 half precision to float32.
func f16(h uint16) float32 {
	sign := uint32(h>>15) << 31
	exp := uint32(h>>10) & 0x1f
	frac := uint32(h) & 0x3ff
	switch exp {
	case 0:
		if frac == 0 {
			return math.Float32frombits(sign)
		}
		// subnormal: normalize
		e := uint32(127 - 15 + 1)
		for frac&0x400 == 0 {
			frac <<= 1
			e--
		}
		frac &= 0x3ff
		return math.Float32frombits(sign | e<<23 | frac<<13)
	case 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | frac<<13)
	}
	return math.Float32frombits(sign | (exp+127-15)<<23 | frac<<13)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
