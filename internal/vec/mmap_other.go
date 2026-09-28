//go:build !unix

package vec

import "os"

// mapFile falls back to reading the file into memory on platforms without
// syscall.Mmap (Windows). Layers are tens of MB, so this is acceptable.
func mapFile(path string) ([]byte, func() error, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	return b, func() error { return nil }, nil
}
