package secretx

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const DefaultMaxBytes int64 = 64 << 10

// Resolve returns an inline value or a value read from a regular file. The two
// sources are mutually exclusive so a stale secret file cannot be shadowed.
func Resolve(name, value, path string, maxBytes int64) (string, error) {
	if value != "" && path != "" {
		return "", fmt.Errorf("%s and %s_file are mutually exclusive", name, name)
	}
	if path == "" {
		return value, nil
	}
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("read %s_file: %w", name, err)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s_file %q is not a regular file", name, path)
	}
	if info.Size() > maxBytes {
		return "", fmt.Errorf("%s_file exceeds %d bytes", name, maxBytes)
	}
	f, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open %s_file: %w", name, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read %s_file: %w", name, err)
	}
	if int64(len(data)) > maxBytes {
		return "", fmt.Errorf("%s_file exceeds %d bytes", name, maxBytes)
	}
	resolved := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if resolved == "" {
		return "", fmt.Errorf("%s_file is empty", name)
	}
	return resolved, nil
}
