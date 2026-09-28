// Package eval measures retrieval quality against judged queries
// (docs/retrieval.md §6). Relevance is judged at document level (a doc id,
// or a chunk id "<doc>#<n>" for chunk-level judgments), binary.
//
// Metrics: Recall@5/@10 (share of relevant docs retrieved), nDCG@10, MRR@10,
// wrong-season@5 (share of top-5 hits from a season other than the pinned
// one — a hard gate that must stay 0), no-match rate, latency p50/p95.
package eval

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/retrieve"
)

// Query is one judged query (eval/queries.jsonl).
type Query struct {
	ID       string   `json:"id"`
	Query    string   `json:"query"`
	Season   string   `json:"frc_season,omitempty"`
	Language string   `json:"language,omitempty"`
	Bucket   string   `json:"bucket"` // howto | symbol | cross-season | troubleshoot | conceptual
	Split    string   `json:"split"`  // train | holdout
	Author   string   `json:"author"` // human | llm
	Relevant []string `json:"relevant"`
	Note     string   `json:"note,omitempty"`
}

// Metrics aggregates a set of queries.
type Metrics struct {
	N            int     `json:"n"`
	Recall5      float64 `json:"recall_at_5"`
	Recall10     float64 `json:"recall_at_10"`
	NDCG10       float64 `json:"ndcg_at_10"`
	MRR10        float64 `json:"mrr_at_10"`
	WrongSeason5 float64 `json:"wrong_season_at_5"`
	NoMatch      float64 `json:"no_match_rate"`
	LowConf      float64 `json:"low_confidence_rate"`
	P50ms        float64 `json:"p50_ms"`
	P95ms        float64 `json:"p95_ms"`
}

// Failure records a query whose first relevant doc is not in the top 10.
type Failure struct {
	ID    string   `json:"id"`
	Query string   `json:"query"`
	Want  []string `json:"want"`
	Got   []string `json:"got"`
}

// Report is an eval run.
type Report struct {
	Overall  Metrics            `json:"overall"`
	ByBucket map[string]Metrics `json:"by_bucket"`
	BySplit  map[string]Metrics `json:"by_split"`
	Failures []Failure          `json:"failures,omitempty"`
}

// Load reads and sanity-checks a queries file.
func Load(path string) ([]Query, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var qs []Query
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<16), 1<<20)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "//") {
			continue
		}
		var q Query
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			return nil, fmt.Errorf("%s:%d: %w", path, n, err)
		}
		if q.ID == "" || seen[q.ID] || q.Query == "" || len(q.Relevant) == 0 {
			return nil, fmt.Errorf("%s:%d: need unique id, query and ≥1 relevant", path, n)
		}
		seen[q.ID] = true
		qs = append(qs, q)
	}
	return qs, sc.Err()
}

// Validate fails when a judged id does not exist in the index, so a typo in
// qrels can never silently depress (or inflate) a metric.
func Validate(ctx context.Context, e *retrieve.Engine, qs []Query) error {
	var bad []string
	for _, q := range qs {
		for _, r := range q.Relevant {
			if !e.DocExists(ctx, r) {
				bad = append(bad, q.ID+": "+r)
			}
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("eval: %d judged ids not in the index:\n  %s", len(bad), strings.Join(bad, "\n  "))
	}
	return nil
}

type result struct {
	q                            Query
	recall5, recall10, ndcg, mrr float64
	wrong5                       float64
	noMatch, lowConf             bool
	lat                          time.Duration
	got                          []string
}

// Run evaluates every query.
func Run(ctx context.Context, e *retrieve.Engine, qs []Query) (*Report, error) {
	var rs []result
	for _, q := range qs {
		t := time.Now()
		res, err := e.Search(ctx, retrieve.Query{Text: q.Query, Season: q.Season, Language: q.Language, Limit: 10})
		lat := time.Since(t)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", q.ID, err)
		}
		r := result{q: q, lat: lat, noMatch: res.Status == retrieve.StatusNoMatch,
			lowConf: res.Status == retrieve.StatusLowConfidence}
		season := res.Season
		var docs []string // ranked unique docs
		wrong := 0
		for i, h := range res.Hits[:min(10, len(res.Hits))] {
			if h.Chunk == nil {
				continue
			}
			if i < 5 && h.Chunk.Season != season {
				wrong++
			}
			id := matchKey(h.Chunk.DocID, h.Chunk.ID(), q.Relevant)
			if !slices.Contains(docs, id) {
				docs = append(docs, id)
			}
			r.got = append(r.got, h.Chunk.ID())
		}
		top5 := min(5, len(res.Hits))
		if top5 > 0 {
			r.wrong5 = float64(wrong) / float64(top5)
		}
		rel := map[string]bool{}
		for _, x := range q.Relevant {
			rel[x] = true
		}
		found5, found10 := 0, 0
		dcg := 0.0
		for i, d := range docs {
			if !rel[d] {
				continue
			}
			if i < 5 {
				found5++
			}
			found10++
			dcg += 1 / math.Log2(float64(i+2))
			if r.mrr == 0 {
				r.mrr = 1 / float64(i+1)
			}
		}
		idcg := 0.0
		for i := range min(len(rel), 10) {
			idcg += 1 / math.Log2(float64(i+2))
		}
		r.recall5 = float64(found5) / float64(len(rel))
		r.recall10 = float64(found10) / float64(len(rel))
		r.ndcg = dcg / idcg
		rs = append(rs, r)
	}
	rep := &Report{Overall: aggregate(rs), ByBucket: map[string]Metrics{}, BySplit: map[string]Metrics{}}
	group := func(key func(result) string, into map[string]Metrics) {
		g := map[string][]result{}
		for _, r := range rs {
			g[key(r)] = append(g[key(r)], r)
		}
		for k, v := range g {
			into[k] = aggregate(v)
		}
	}
	group(func(r result) string { return r.q.Bucket }, rep.ByBucket)
	group(func(r result) string { return r.q.Split }, rep.BySplit)
	for _, r := range rs {
		if r.mrr == 0 {
			rep.Failures = append(rep.Failures, Failure{ID: r.q.ID, Query: r.q.Query, Want: r.q.Relevant, Got: r.got[:min(5, len(r.got))]})
		}
	}
	return rep, nil
}

// matchKey maps a hit to the judgment key it satisfies: chunk-level
// judgments match the chunk id, otherwise the doc id.
func matchKey(docID, chunkID string, relevant []string) string {
	if slices.Contains(relevant, chunkID) {
		return chunkID
	}
	return docID
}

func aggregate(rs []result) Metrics {
	m := Metrics{N: len(rs)}
	if len(rs) == 0 {
		return m
	}
	lats := make([]float64, 0, len(rs))
	for _, r := range rs {
		m.Recall5 += r.recall5
		m.Recall10 += r.recall10
		m.NDCG10 += r.ndcg
		m.MRR10 += r.mrr
		m.WrongSeason5 += r.wrong5
		if r.noMatch {
			m.NoMatch++
		}
		if r.lowConf {
			m.LowConf++
		}
		lats = append(lats, float64(r.lat.Microseconds())/1000)
	}
	n := float64(len(rs))
	m.Recall5, m.Recall10, m.NDCG10, m.MRR10 = round(m.Recall5/n), round(m.Recall10/n), round(m.NDCG10/n), round(m.MRR10/n)
	m.WrongSeason5, m.NoMatch, m.LowConf = round(m.WrongSeason5/n), round(m.NoMatch/n), round(m.LowConf/n)
	sort.Float64s(lats)
	m.P50ms = round(lats[len(lats)/2])
	m.P95ms = round(lats[int(0.95*float64(len(lats)-1))])
	return m
}

func round(f float64) float64 { return math.Round(f*1000) / 1000 }
