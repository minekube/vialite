package vialite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type assetKind int

const (
	assetKindBinary assetKind = iota
	assetKindLibrary
)

var (
	runtimeGOOS   = runtime.GOOS
	runtimeGOARCH = runtime.GOARCH
)

func downloadAsset(ctx context.Context, opts Options, kind assetKind) (string, error) {
	assetName, err := assetForRuntime(kind)
	if err != nil {
		return "", err
	}
	version, err := resolveDownloadVersion(ctx, opts)
	if err != nil {
		return "", err
	}
	base := opts.Mirror
	if base == "" {
		base = DefaultDownloadBase
	}
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("vialite: locate cache: %w", err)
	}
	dir := filepath.Join(cacheDir, "vialite", version)
	expectedSha, err := fetchExpectedSha(ctx, base, version, assetName)
	if err != nil {
		return "", err
	}
	cachedPath := verifiedDownloadPath(dir, assetName, expectedSha)
	if existing, err := os.Open(cachedPath); err == nil {
		sum, hashErr := streamSha(existing)
		_ = existing.Close()
		if hashErr == nil && sum == expectedSha {
			return cachedPath, nil
		}
	}
	if err := os.MkdirAll(filepath.Dir(cachedPath), 0o755); err != nil {
		return "", fmt.Errorf("vialite: mkdir cache: %w", err)
	}
	url := fmt.Sprintf("%s/%s/%s", strings.TrimSuffix(base, "/"), version, assetName)
	tmp, err := os.CreateTemp(filepath.Dir(cachedPath), assetName+".*.tmp")
	if err != nil {
		return "", fmt.Errorf("vialite: create temp download: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	if err := downloadFile(ctx, url, tmpPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	gotSha, err := shaFile(tmpPath)
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if gotSha != expectedSha {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("vialite: sha256 mismatch for %s: got %s, want %s", assetName, gotSha, expectedSha)
	}
	if kind == assetKindBinary {
		_ = os.Chmod(tmpPath, 0o755)
	}
	if err := os.Rename(tmpPath, cachedPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("vialite: rename %s: %w", cachedPath, err)
	}
	return cachedPath, nil
}

func resolveDownloadVersion(ctx context.Context, opts Options) (string, error) {
	version := strings.TrimSpace(opts.Version)
	if version == "" && opts.Mirror != "" {
		return DefaultMirrorVersion, nil
	}
	if !latestVersionRequested(version) {
		return version, nil
	}
	url := DefaultLatestReleaseURL
	if opts.Mirror != "" {
		url = strings.TrimSuffix(opts.Mirror, "/") + "/latest"
	}
	version, err := fetchLatestReleaseTag(ctx, url)
	if err != nil {
		return "", err
	}
	return version, nil
}

func latestVersionRequested(version string) bool {
	return version == "" || strings.EqualFold(version, "auto") || strings.EqualFold(version, "latest")
}

func fetchLatestReleaseTag(ctx context.Context, url string) (string, error) {
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("vialite: fetch latest release: %w", err)
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, 64<<10))
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.Unmarshal(data, &release); err != nil {
		return "", fmt.Errorf("vialite: parse latest release metadata: %w", err)
	}
	tag := strings.TrimSpace(release.TagName)
	if tag == "" {
		return "", errors.New("vialite: latest release metadata missing tag_name")
	}
	return tag, nil
}

func verifiedDownloadPath(dir, assetName, expectedSha string) string {
	return filepath.Join(dir, expectedSha, assetName)
}

func assetForRuntime(kind assetKind) (string, error) {
	return assetFor(kind, runtimeGOOS, runtimeGOARCH)
}

func assetFor(kind assetKind, goos, goarch string) (string, error) {
	var arch string
	switch goarch {
	case "amd64", "arm64":
		arch = goarch
	default:
		return "", fmt.Errorf("vialite: auto-download supports amd64/arm64 only (got %s)", goarch)
	}

	if goos == "windows" {
		if kind == assetKindBinary && arch == "amd64" {
			return "vialite-windows-amd64.exe", nil
		}
		return "", fmt.Errorf("vialite: auto-download supports windows/amd64 subprocess binaries only (got %s/%s); set BinaryPath/LibraryPath manually", goos, goarch)
	}
	if goos != "linux" {
		return "", fmt.Errorf("vialite: auto-download supports linux amd64/arm64 and windows amd64 subprocess binaries only (got %s/%s); set BinaryPath/LibraryPath manually", goos, goarch)
	}

	switch kind {
	case assetKindBinary:
		return fmt.Sprintf("vialite-linux-%s", arch), nil
	case assetKindLibrary:
		return fmt.Sprintf("libvialite-linux-%s.so", arch), nil
	default:
		return "", errors.New("vialite: unknown asset kind")
	}
}

func fetchExpectedSha(ctx context.Context, base, version, assetName string) (string, error) {
	url := fmt.Sprintf("%s/%s/checksums.txt", strings.TrimSuffix(base, "/"), version)
	body, err := httpGet(ctx, url)
	if err != nil {
		return "", fmt.Errorf("vialite: fetch checksums for %s: %w", version, err)
	}
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, 1<<20))
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName || strings.HasSuffix(name, "/"+assetName) {
			sha := strings.ToLower(fields[0])
			if !isSHA256Hex(sha) {
				return "", fmt.Errorf("%w for %s: %q", ErrInvalidChecksum, assetName, fields[0])
			}
			return sha, nil
		}
	}
	return "", fmt.Errorf("vialite: %s not listed in checksums.txt for %s", assetName, version)
}

func isSHA256Hex(s string) bool {
	if len(s) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func downloadFile(ctx context.Context, url, dest string) error {
	body, err := httpGet(ctx, url)
	if err != nil {
		return fmt.Errorf("vialite: get %s: %w", url, err)
	}
	defer func() { _ = body.Close() }()
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, body); err != nil {
		_ = f.Close()
		return fmt.Errorf("vialite: copy %s: %w", dest, err)
	}
	return f.Close()
}

func httpGet(ctx context.Context, url string) (io.ReadCloser, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode/100 != 2 {
		_ = resp.Body.Close()
		return nil, fmt.Errorf("http %d for %s", resp.StatusCode, url)
	}
	return resp.Body, nil
}

func shaFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	return streamSha(f)
}

func streamSha(r io.Reader) (string, error) {
	h := sha256.New()
	if _, err := io.Copy(h, r); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
