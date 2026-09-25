package vialite

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
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

func TestDownloadAssetAutoVersionUsesLatestRelease(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	const sha = "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	var sawLatest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			sawLatest = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
		case "/v9.9.9/checksums.txt":
			_, _ = w.Write([]byte(sha + "  vialite-linux-arm64\n"))
		case "/v9.9.9/vialite-linux-arm64":
			_, _ = w.Write([]byte(body))
		case "/" + DefaultMirrorVersion + "/checksums.txt":
			t.Fatalf("explicit auto version used DefaultMirrorVersion instead of latest release")
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Version: "auto", Mirror: srv.URL}, assetKindBinary)
	if err != nil {
		t.Fatalf("downloadAsset latest: %v", err)
	}
	if !sawLatest {
		t.Fatal("downloadAsset did not request latest release metadata")
	}
	if !strings.Contains(path, filepath.Join("vialite", "v9.9.9", sha)) {
		t.Fatalf("download path = %q, want resolved latest version and checksum", path)
	}
}

// An unset version with a custom mirror must follow the mirror's own latest
// release, exactly like "auto"/"latest" do. Before this behaviour existed the
// mirror silently pinned the compiled-in DefaultMirrorVersion, so the operator
// kept running a stale runtime (and an old ViaVersion ceiling) forever.
func TestDownloadAssetEmptyVersionWithMirrorUsesMirrorLatest(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	const sha = "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	var sawLatest bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			sawLatest = true
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"tag_name":"v9.9.9"}`))
		case "/v9.9.9/checksums.txt":
			_, _ = w.Write([]byte(sha + "  vialite-linux-arm64\n"))
		case "/v9.9.9/vialite-linux-arm64":
			_, _ = w.Write([]byte(body))
		case "/" + DefaultMirrorVersion + "/checksums.txt":
			t.Fatalf("empty mirror version silently used the pinned fallback %s instead of the mirror's latest release", DefaultMirrorVersion)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Mirror: srv.URL}, assetKindBinary)
	if err != nil {
		t.Fatalf("downloadAsset empty mirror version: %v", err)
	}
	if !sawLatest {
		t.Fatal("downloadAsset with an empty version did not ask the mirror for its latest release")
	}
	if !strings.Contains(path, filepath.Join("vialite", "v9.9.9", sha)) {
		t.Fatalf("download path = %q, want the mirror's latest version and checksum", path)
	}
}

// A mirror that cannot report a latest release (a plain file mirror) must keep
// working, but it must not do so silently: the operator gets a loud warning
// naming the pinned fallback version and how to get newest instead.
func TestDownloadAssetEmptyVersionWithSilentMirrorWarnsAndFallsBack(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	const sha = "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			http.NotFound(w, r)
		case "/" + DefaultMirrorVersion + "/checksums.txt":
			_, _ = w.Write([]byte(sha + "  vialite-linux-arm64\n"))
		case "/" + DefaultMirrorVersion + "/vialite-linux-arm64":
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Mirror: srv.URL, Logger: logger}, assetKindBinary)
	if err != nil {
		t.Fatalf("downloadAsset empty mirror version: %v", err)
	}
	if !strings.Contains(path, filepath.Join("vialite", DefaultMirrorVersion, sha)) {
		t.Fatalf("download path = %q, want default mirror version and checksum", path)
	}
	line := logs.String()
	if !strings.Contains(line, "level=WARN") {
		t.Fatalf("mirror fallback was silent, logs = %q", line)
	}
	if !strings.Contains(line, "pinned fallback runtime") {
		t.Fatalf("mirror fallback warning does not name the fallback, logs = %q", line)
	}
	if !strings.Contains(line, DefaultMirrorVersion) {
		t.Fatalf("mirror fallback warning does not name %s, logs = %q", DefaultMirrorVersion, line)
	}
}

// An explicit latest/auto request against a mirror that cannot answer is a hard
// error: the operator asked for newest, and quietly downgrading would be worse
// than failing the start.
func TestDownloadAssetExplicitLatestWithSilentMirrorFails(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	if _, err := downloadAsset(context.Background(), Options{Version: "latest", Mirror: srv.URL}, assetKindBinary); err == nil {
		t.Fatal("downloadAsset explicit latest with a silent mirror succeeded, want a hard error")
	}
}

// Support and operators need one startup line naming the runtime that is
// actually running and where it came from: the artifact version decides the
// ViaVersion protocol ceiling, and nothing else in the logs names it.
func TestDownloadAssetLogsResolvedRuntimeVersionAndSource(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "amd64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	const sha = "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v9.9.9/checksums.txt":
			_, _ = w.Write([]byte(sha + "  vialite-linux-amd64\n"))
		case "/v9.9.9/vialite-linux-amd64":
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	opts := Options{Version: "v9.9.9", Mirror: srv.URL, Logger: logger}

	path, err := downloadAsset(context.Background(), opts, assetKindBinary)
	if err != nil {
		t.Fatalf("downloadAsset: %v", err)
	}
	downloaded := logs.String()
	for _, want := range []string{"vialite: resolved runtime", "kind=binary", "source=download", "version=v9.9.9", path} {
		if !strings.Contains(downloaded, want) {
			t.Fatalf("download log line %q does not contain %q", downloaded, want)
		}
	}

	logs.Reset()
	if _, err := downloadAsset(context.Background(), opts, assetKindBinary); err != nil {
		t.Fatalf("downloadAsset cached: %v", err)
	}
	cached := logs.String()
	for _, want := range []string{"vialite: resolved runtime", "source=cache", "version=v9.9.9", path} {
		if !strings.Contains(cached, want) {
			t.Fatalf("cache log line %q does not contain %q", cached, want)
		}
	}
}

func TestDownloadAssetLatestAliasesUseLatestRelease(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "amd64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-library"
	const sha = "01307e18b53bf651632b9119874fdff0771bfe2f2dafc10af8a901b394842a70"
	for _, version := range []string{"auto", "latest", " Latest "} {
		t.Run(version, func(t *testing.T) {
			var sawLatest bool
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/latest":
					sawLatest = true
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(`{"tag_name":"v8.0.0"}`))
				case "/v8.0.0/checksums.txt":
					_, _ = w.Write([]byte(sha + "  libvialite-linux-amd64.so\n"))
				case "/v8.0.0/libvialite-linux-amd64.so":
					_, _ = w.Write([]byte(body))
				default:
					http.NotFound(w, r)
				}
			}))
			defer srv.Close()

			cache := t.TempDir()
			t.Setenv("XDG_CACHE_HOME", cache)
			path, err := downloadAsset(context.Background(), Options{Version: version, Mirror: srv.URL}, assetKindLibrary)
			if err != nil {
				t.Fatalf("downloadAsset alias %q: %v", version, err)
			}
			if !sawLatest {
				t.Fatalf("downloadAsset alias %q did not request latest release metadata", version)
			}
			if !strings.Contains(path, filepath.Join("vialite", "v8.0.0", sha)) {
				t.Fatalf("download path = %q, want resolved latest version and checksum", path)
			}
		})
	}
}

func TestDownloadAssetUnsupportedRuntimeSkipsLatestRelease(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "darwin", "amd64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/latest" {
			t.Fatalf("unsupported runtime unexpectedly requested latest release metadata")
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	_, err := downloadAsset(context.Background(), Options{Version: "auto", Mirror: srv.URL}, assetKindLibrary)
	if err == nil {
		t.Fatal("downloadAsset unsupported runtime succeeded")
	}
	if !strings.Contains(err.Error(), "auto-download supports linux") {
		t.Fatalf("downloadAsset error = %v, want unsupported-platform error", err)
	}
}

func TestDownloadAssetPinnedVersionSkipsLatestRelease(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	const sha = "9ec4c62cbabe2558224228ab3254a4e20e24cdf57a2cf3be50f37111723595e5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/latest":
			t.Fatalf("pinned version unexpectedly requested latest release metadata")
		case "/v1.2.3/checksums.txt":
			_, _ = w.Write([]byte(sha + "  vialite-linux-arm64\n"))
		case "/v1.2.3/vialite-linux-arm64":
			_, _ = w.Write([]byte(body))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	path, err := downloadAsset(context.Background(), Options{Version: "v1.2.3", Mirror: srv.URL}, assetKindBinary)
	if err != nil {
		t.Fatalf("downloadAsset pinned: %v", err)
	}
	if !strings.Contains(path, filepath.Join("vialite", "v1.2.3", sha)) {
		t.Fatalf("download path = %q, want pinned version and checksum", path)
	}
}
