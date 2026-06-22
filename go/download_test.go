package vialite

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAssetFor(t *testing.T) {
	tests := []struct {
		name   string
		kind   assetKind
		goos   string
		goarch string
		want   string
	}{
		{"linux amd64 library", assetKindLibrary, "linux", "amd64", "libvialite-linux-amd64.so"},
		{"linux arm64 library", assetKindLibrary, "linux", "arm64", "libvialite-linux-arm64.so"},
		{"linux amd64 binary", assetKindBinary, "linux", "amd64", "vialite-linux-amd64"},
		{"linux arm64 binary", assetKindBinary, "linux", "arm64", "vialite-linux-arm64"},
		{"windows amd64 binary", assetKindBinary, "windows", "amd64", "vialite-windows-amd64.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := assetFor(tt.kind, tt.goos, tt.goarch)
			if err != nil {
				t.Fatalf("assetFor returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("assetFor = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestAssetForUnsupported(t *testing.T) {
	tests := []struct {
		name   string
		kind   assetKind
		goos   string
		goarch string
	}{
		{"darwin library", assetKindLibrary, "darwin", "arm64"},
		{"darwin binary", assetKindBinary, "darwin", "arm64"},
		{"windows library", assetKindLibrary, "windows", "amd64"},
		{"windows arm64 binary", assetKindBinary, "windows", "arm64"},
		{"linux 386 binary", assetKindBinary, "linux", "386"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := assetFor(tt.kind, tt.goos, tt.goarch); err == nil {
				t.Fatalf("assetFor(%v, %q, %q) returned nil error", tt.kind, tt.goos, tt.goarch)
			}
		})
	}
}

func TestFetchExpectedSha(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0.1.0/checksums.txt" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(sha + "  libvialite-linux-amd64.so\n"))
	}))
	defer srv.Close()

	got, err := fetchExpectedSha(context.Background(), srv.URL, "v0.1.0", "libvialite-linux-amd64.so")
	if err != nil {
		t.Fatalf("fetchExpectedSha: %v", err)
	}
	if got != sha {
		t.Fatalf("sha = %q", got)
	}
}

func TestFetchExpectedShaRejectsMalformedSHA256(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not-a-sha  libvialite-linux-amd64.so\n"))
	}))
	defer srv.Close()

	_, err := fetchExpectedSha(context.Background(), srv.URL, "v0.1.0", "libvialite-linux-amd64.so")
	if !errors.Is(err, ErrInvalidChecksum) {
		t.Fatalf("fetchExpectedSha malformed sha = %v, want ErrInvalidChecksum", err)
	}
}

func TestVerifiedDownloadPath(t *testing.T) {
	got := verifiedDownloadPath("/cache", "vialite-linux-amd64", "abc123")
	want := filepath.Join("/cache", "abc123", "vialite-linux-amd64")
	if got != want {
		t.Fatalf("path = %q, want %q", got, want)
	}
}

func TestDownloadAssetVerifiesChecksum(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "amd64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-library"
	sha := "01307e18b53bf651632b9119874fdff0771bfe2f2dafc10af8a901b394842a70"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(sha + "  libvialite-linux-amd64.so\n"))
		case strings.HasSuffix(r.URL.Path, "/libvialite-linux-amd64.so"):
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Version: "v0.1.0", Mirror: srv.URL}, assetKindLibrary)
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read downloaded asset: %v", err)
	}
	if string(data) != body {
		t.Fatalf("asset body = %q", data)
	}
	if !strings.Contains(path, sha) {
		t.Fatalf("download path %q does not contain sha %q", path, sha)
	}
}

func TestDownloadAssetLibraryUsesPlatformNamedReleaseAsset(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "amd64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-library"
	sha := "01307e18b53bf651632b9119874fdff0771bfe2f2dafc10af8a901b394842a70"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(sha + "  libvialite-linux-amd64.so\n"))
		case strings.HasSuffix(r.URL.Path, "/libvialite-linux-amd64.so"):
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/libvialite.so"):
			t.Fatalf("download used unqualified libvialite.so release asset")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Version: "v0.1.0", Mirror: srv.URL}, assetKindLibrary)
	if err != nil {
		t.Fatalf("downloadAsset library: %v", err)
	}
	if filepath.Base(path) != "libvialite-linux-amd64.so" {
		t.Fatalf("downloaded path base = %q", filepath.Base(path))
	}
}

func TestDownloadAssetBinaryUsesPlatformNamedReleaseAsset(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	sha := "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(sha + "  vialite-linux-arm64\n"))
		case strings.HasSuffix(r.URL.Path, "/vialite-linux-arm64"):
			_, _ = w.Write([]byte(body))
		case strings.HasSuffix(r.URL.Path, "/vialite"):
			t.Fatalf("download used unqualified vialite release asset")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Version: "v0.1.0", Mirror: srv.URL}, assetKindBinary)
	if err != nil {
		t.Fatalf("downloadAsset binary: %v", err)
	}
	if filepath.Base(path) != "vialite-linux-arm64" {
		t.Fatalf("downloaded path base = %q", filepath.Base(path))
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat downloaded binary: %v", err)
	}
	if info.Mode()&0o111 == 0 {
		t.Fatalf("downloaded binary mode = %v, want executable bit", info.Mode())
	}
}
