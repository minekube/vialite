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
		kind   assetKind
		goos   string
		goarch string
		want   string
	}{
		{assetKindBinary, "linux", "amd64", "vialite-linux-amd64"},
		{assetKindBinary, "linux", "arm64", "vialite-linux-arm64"},
		{assetKindLibrary, "linux", "amd64", "libvialite-linux-amd64.so"},
		{assetKindLibrary, "linux", "arm64", "libvialite-linux-arm64.so"},
		{assetKindBinary, "windows", "amd64", "vialite-windows-amd64.exe"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
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
	if _, err := assetFor(assetKindLibrary, "darwin", "arm64"); err == nil {
		t.Fatal("assetFor darwin library returned nil error")
	}
	if _, err := assetFor(assetKindBinary, "linux", "386"); err == nil {
		t.Fatal("assetFor linux/386 returned nil error")
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

	const body = "native-binary"
	sha := "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/checksums.txt"):
			_, _ = w.Write([]byte(sha + "  vialite-linux-amd64\n"))
		case strings.HasSuffix(r.URL.Path, "/vialite-linux-amd64"):
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Version: "v0.1.0", Mirror: srv.URL}, assetKindBinary)
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
