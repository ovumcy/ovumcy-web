package security

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReadBoundedRegularFile(path string, label string, maxBytes int64) ([]byte, error) {
	rawPath := strings.TrimSpace(path)
	cleanPath := filepath.Clean(rawPath)
	if rawPath == "" || cleanPath == "." {
		return nil, fmt.Errorf("%s must reference a regular file", label)
	}

	info, err := os.Stat(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %s: %w", label, rawPath, err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s must reference a regular file", label)
	}
	if maxBytes > 0 && info.Size() > maxBytes {
		return nil, fmt.Errorf("%s must be at most %d bytes", label, maxBytes)
	}

	// #nosec G304 -- caller-supplied path is operator-managed local startup/runtime configuration and validated as a regular file before read.
	file, err := os.Open(cleanPath)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %s: %w", label, rawPath, err)
	}
	defer file.Close()

	reader := io.Reader(file)
	if maxBytes > 0 {
		reader = io.LimitReader(file, maxBytes+1)
	}
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("%s could not be read: %s: %w", label, rawPath, err)
	}
	if maxBytes > 0 && int64(len(content)) > maxBytes {
		return nil, fmt.Errorf("%s must be at most %d bytes", label, maxBytes)
	}
	return content, nil
}
