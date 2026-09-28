package index

import (
	"context"
	"slices"
)

// RowMeta is a compact, in-memory copy of the per-chunk fields needed to
// pre-filter and boost dense (vector) candidates without touching SQLite:
// one byte per enum field, dictionary-coded strings. Index i describes row
// i+1 (= vector layer row i). 100k chunks ≈ 1 MB.
type RowMeta struct {
	Seasons, Libraries []string // dictionaries
	Season             []uint8
	Library            []uint16
	Language           []uint8 // index into Languages
	Kind               []uint8 // KindProse …
	Trust              []uint8 // TrustOfficial …
	Alpha, Suspect     []bool
	DocNum             []int32
}

// RowMeta loads the compact metadata for every chunk, ordered by row id.
func (r *Reader) RowMeta(ctx context.Context) (*RowMeta, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, season, library, language, kind, trust, channel = 'alpha', suspect, doc_num FROM chunk ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := &RowMeta{}
	for rows.Next() {
		var id int64
		var season, lib, lang, kind, trust string
		var alpha, suspect bool
		var doc int32
		if err := rows.Scan(&id, &season, &lib, &lang, &kind, &trust, &alpha, &suspect, &doc); err != nil {
			return nil, err
		}
		for int64(len(m.Season)) < id-1 { // keep alignment even with gaps
			m.appendBlank()
		}
		m.Season = append(m.Season, uint8(dict(&m.Seasons, season)))
		m.Library = append(m.Library, uint16(dict(&m.Libraries, lib)))
		m.Language = append(m.Language, uint8(slices.Index(Languages, lang)))
		m.Kind = append(m.Kind, uint8(slices.Index(Kinds, kind)))
		m.Trust = append(m.Trust, uint8(slices.Index(Trusts, trust)))
		m.Alpha = append(m.Alpha, alpha)
		m.Suspect = append(m.Suspect, suspect)
		m.DocNum = append(m.DocNum, doc)
	}
	return m, rows.Err()
}

func (m *RowMeta) appendBlank() {
	m.Season = append(m.Season, 255)
	m.Library = append(m.Library, 65535)
	m.Language = append(m.Language, 255)
	m.Kind = append(m.Kind, 255)
	m.Trust = append(m.Trust, 255)
	m.Alpha = append(m.Alpha, false)
	m.Suspect = append(m.Suspect, false)
	m.DocNum = append(m.DocNum, -1)
}

func dict(d *[]string, v string) int {
	if i := slices.Index(*d, v); i >= 0 {
		return i
	}
	*d = append(*d, v)
	return len(*d) - 1
}

// Len is the number of rows described.
func (m *RowMeta) Len() int { return len(m.Season) }

// Allow builds the bitset of rows passing f (same semantics as the SQL
// filter). A nil result means "no row passes".
func (m *RowMeta) Allow(f Filter) []uint64 {
	n := m.Len()
	bits := make([]uint64, (n+63)/64)
	season := -1
	if f.Season != "" {
		if season = slices.Index(m.Seasons, f.Season); season < 0 {
			return bits
		}
	}
	lang := -1
	if f.Language != "" && f.Language != "any" {
		lang = slices.Index(Languages, f.Language)
	}
	anyLang := uint8(slices.Index(Languages, "any"))
	var libs []uint16
	for _, l := range f.Libraries {
		if i := slices.Index(m.Libraries, l); i >= 0 {
			libs = append(libs, uint16(i))
		}
	}
	if len(f.Libraries) > 0 && len(libs) == 0 {
		return bits
	}
	var kinds []uint8
	for _, k := range f.Kinds {
		kinds = append(kinds, uint8(slices.Index(Kinds, k)))
	}
	for i := 0; i < n; i++ {
		if m.Season[i] == 255 ||
			(season >= 0 && int(m.Season[i]) != season) ||
			(lang >= 0 && int(m.Language[i]) != lang && m.Language[i] != anyLang) ||
			(!f.IncludeCommunity && m.Trust[i] == TrustCommunity) ||
			(len(libs) > 0 && !slices.Contains(libs, m.Library[i])) ||
			(len(kinds) > 0 && !slices.Contains(kinds, m.Kind[i])) {
			continue
		}
		bits[i>>6] |= 1 << (uint(i) & 63)
	}
	return bits
}
