//go:build vialite_embed && linux && (amd64 || arm64)

package vialite

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
)

func extractBytes(name string, data []byte, mode os.FileMode) (string, bool, error) {
	if len(data) == 0 {
		return "", false, nil
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", false, err
	}
	sum := sha256.Sum256(data)
	dir := filepath.Join(cacheDir, "vialite", "embedded", hex.EncodeToString(sum[:]))
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", false, err
	}
	if existing, err := os.ReadFile(path); err == nil && string(existing) == string(data) {
		return path, true, nil
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		return "", false, err
	}
	return path, true, nil
}
