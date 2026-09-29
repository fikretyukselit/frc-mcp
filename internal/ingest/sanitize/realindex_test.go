package sanitize

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// TestRealIndexNotSuspect runs the suspect detector over every chunk of the
// real index built by `make index` (skipped without it, or with -short):
// official and vendor documentation must not be flagged.
func TestRealIndexNotSuspect(t *testing.T) {
	if testing.Short() {
		t.Skip("-short")
	}
	paths, _ := filepath.Glob("../../../.shards/*.sqlite")
	if len(paths) == 0 {
		t.Skip("no ../../../.shards; run `make index`")
	}
	total := 0
	for _, p := range paths {
		db, err := sql.Open("sqlite", "file:"+p+"?mode=ro")
		if err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query(`SELECT doc_id, ord, title, body FROM chunk`)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var id, title, body string
			var ord int
			if err := rows.Scan(&id, &ord, &title, &body); err != nil {
				t.Fatal(err)
			}
			total++
			if Suspect(title) || Suspect(body) {
				for _, re := range suspectPatterns {
					if loc := re.FindStringIndex(body); loc != nil {
						t.Errorf("%s#%d flagged: …%q…", id, ord, body[max(0, loc[0]-60):min(len(body), loc[1]+60)])
						break
					}
				}
			}
		}
		rows.Close()
		db.Close()
	}
	t.Logf("%d chunks checked", total)
}
