// Package pypi ingests a Python package's API from one wheel on PyPI.
// src.URL is the version-pinned JSON API (https://pypi.org/pypi/<pkg>/<ver>/json);
// the adapter picks one wheel (pure-Python first, then CPython manylinux
// x86_64: the stubs are identical across platforms) and hands it to pystub.
package pypi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fikretyukselit/frc-mcp/internal/index"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/fetch"
	"github.com/fikretyukselit/frc-mcp/internal/ingest/source/pystub"
	"github.com/fikretyukselit/frc-mcp/internal/sources"
)

// Getter is the fetch dependency (fetch.Fetcher in production).
type Getter interface {
	Get(ctx context.Context, url string) (*fetch.Result, error)
}

type file struct {
	Filename    string `json:"filename"`
	URL         string `json:"url"`
	PackageType string `json:"packagetype"`
	Digests     struct {
		SHA256 string `json:"sha256"`
	} `json:"digests"`
}

type release struct {
	Info struct {
		Version string `json:"version"`
	} `json:"info"`
	URLs []file `json:"urls"`
}

// ErrNoWheel means the release has no usable wheel.
var ErrNoWheel = errors.New("pypi: no wheel")

// PickWheel chooses the wheel to read: py3-none-any, else the newest CPython
// manylinux x86_64 build, else any wheel (sorted for determinism).
func PickWheel(files []file) (file, bool) {
	var whl []file
	for _, f := range files {
		if f.PackageType == "bdist_wheel" && strings.HasSuffix(f.Filename, ".whl") && strings.HasPrefix(f.URL, "https://") {
			whl = append(whl, f)
		}
	}
	if len(whl) == 0 {
		return file{}, false
	}
	sort.Slice(whl, func(i, j int) bool { return whl[i].Filename > whl[j].Filename })
	for _, f := range whl {
		if strings.Contains(f.Filename, "-none-any.whl") {
			return f, true
		}
	}
	for _, f := range whl {
		if strings.Contains(f.Filename, "manylinux") && strings.Contains(f.Filename, "x86_64") {
			return f, true
		}
	}
	return whl[0], true
}

// Parse reads the JSON at jsonPath, downloads the chosen wheel (cached,
// digest-checked) and emits its symbols and API chunks.
func Parse(ctx context.Context, g Getter, jsonPath string, src sources.Source, retrieved time.Time,
	emitSymbol func(index.Symbol) error, emitChunk func(index.Chunk) error) (pystub.Stats, error) {
	b, err := os.ReadFile(jsonPath)
	if err != nil {
		return pystub.Stats{}, err
	}
	var rel release
	if err := json.Unmarshal(b, &rel); err != nil {
		return pystub.Stats{}, fmt.Errorf("pypi: %s: %w", src.URL, err)
	}
	w, ok := PickWheel(rel.URLs)
	if !ok {
		return pystub.Stats{}, fmt.Errorf("%w in %s", ErrNoWheel, src.URL)
	}
	res, err := g.Get(ctx, w.URL)
	if err != nil {
		return pystub.Stats{}, fmt.Errorf("pypi: %s: %w", w.Filename, err)
	}
	if w.Digests.SHA256 != "" && res.SHA256 != w.Digests.SHA256 {
		return pystub.Stats{}, fmt.Errorf("pypi: %s: sha256 %s, PyPI says %s", w.Filename, res.SHA256, w.Digests.SHA256)
	}
	return pystub.Parse(res.Path, src, w.Filename, retrieved, emitSymbol, emitChunk)
}
