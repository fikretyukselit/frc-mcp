package index

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// BuildFromJSONL writes a shard from newline-delimited JSON chunk and symbol
// files. It is the M0 ingestion path (fixtures, hand-curated data) and the
// sink the adapter pipeline writes through in later milestones. Either input
// path may be empty.
func BuildFromJSONL(ctx context.Context, out, name, chunksPath, symbolsPath string) (Meta, error) {
	w, err := Create(ctx, out, name)
	if err != nil {
		return Meta{}, err
	}
	fail := func(err error) (Meta, error) { w.abort(); return Meta{}, err }
	if chunksPath != "" {
		if err := eachJSONL(chunksPath, func(line int, b []byte) error {
			var c Chunk
			if err := json.Unmarshal(b, &c); err != nil {
				return fmt.Errorf("%s:%d: %w", chunksPath, line, err)
			}
			if err := w.AddChunk(ctx, c); err != nil {
				return fmt.Errorf("%s:%d: %w", chunksPath, line, err)
			}
			return nil
		}); err != nil {
			return fail(err)
		}
	}
	if symbolsPath != "" {
		if err := eachJSONL(symbolsPath, func(line int, b []byte) error {
			var s Symbol
			if err := json.Unmarshal(b, &s); err != nil {
				return fmt.Errorf("%s:%d: %w", symbolsPath, line, err)
			}
			if err := w.AddSymbol(ctx, s); err != nil {
				return fmt.Errorf("%s:%d: %w", symbolsPath, line, err)
			}
			return nil
		}); err != nil {
			return fail(err)
		}
	}
	if err := w.Close(ctx); err != nil {
		return Meta{}, err
	}
	r, err := Open(ctx, out)
	if err != nil {
		return Meta{}, err
	}
	defer r.Close()
	return r.Meta(), nil
}

func eachJSONL(path string, fn func(line int, b []byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 16<<20)
	for n := 1; sc.Scan(); n++ {
		b := sc.Bytes()
		if len(b) == 0 || b[0] == '/' { // allow "//" comment lines in hand-written fixtures
			continue
		}
		if err := fn(n, b); err != nil {
			return err
		}
	}
	return sc.Err()
}
