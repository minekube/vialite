# vialite Artifact Parity Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `vialite` publish and resolve the same practical artifact matrix as `geyserlite`: Linux amd64/arm64 embedded libraries and subprocess binaries, plus Windows amd64 subprocess binary.

**Architecture:** Keep artifact resolution in the Go module and release/build responsibility in GitHub Actions. The native Linux Docker build should stage both `libvialite-linux-$arch.so` and `vialite-linux-$arch`; the Windows job should stage `vialite-windows-amd64.exe`. Release should collect native-image workflow artifacts and generate one checksummed GitHub release instead of rebuilding a single amd64 shared library.

**Tech Stack:** Go, GitHub Actions, Docker Buildx, GraalVM native-image, Gradle.

---

## File Map

- Modify `go/download.go`: allow subprocess auto-download for Linux amd64/arm64 and Windows amd64.
- Modify `go/download_test.go`: lock supported and unsupported asset names, and prove binary auto-download uses platform-qualified assets.
- Modify `go/assets/README.md`: document the full release/embed asset layout.
- Modify `build/Dockerfile`: add a Linux executable artifact alongside the existing shared library artifact.
- Modify `.github/workflows/native-image.yml`: build Linux amd64/arm64 artifacts and Windows amd64 executable artifacts.
- Modify `.github/workflows/release.yml`: collect native-image artifacts for the release commit, generate checksums, publish all assets.
- Optionally modify `README.md`: update the shipped artifact status if the current wording is too narrow after implementation.

## Task 1: Go Asset Resolver Parity

**Files:**
- Modify: `go/download.go`
- Modify: `go/download_test.go`

- [ ] **Step 1: Expand the failing resolver tests**

Replace `TestAssetFor` and `TestAssetForUnsupported` in `go/download_test.go` with:

```go
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
```

- [ ] **Step 2: Add a binary download regression test**

Append this test to `go/download_test.go` after `TestDownloadAssetLibraryUsesPlatformNamedReleaseAsset`:

```go
func TestDownloadAssetBinaryUsesPlatformNamedReleaseAsset(t *testing.T) {
	oldGOOS, oldGOARCH := runtimeGOOS, runtimeGOARCH
	runtimeGOOS, runtimeGOARCH = "linux", "arm64"
	t.Cleanup(func() {
		runtimeGOOS, runtimeGOARCH = oldGOOS, oldGOARCH
	})

	const body = "native-binary"
	sha := "c50e311a28b16bc879b06018e1ebf8cc117feece459d7331f22bb193a5f12d78"
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
```

- [ ] **Step 3: Run the targeted test and confirm it fails**

Run:

```bash
go -C go test ./... -run 'TestAssetFor|TestDownloadAssetBinaryUsesPlatformNamedReleaseAsset'
```

Expected: `TestAssetFor/linux amd64 binary` fails because subprocess binaries are not currently published by `assetFor`.

- [ ] **Step 4: Implement resolver parity**

Replace `assetFor` in `go/download.go` with:

```go
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
```

- [ ] **Step 5: Run Go tests**

Run:

```bash
go -C go test ./...
```

Expected: all Go tests pass.

- [ ] **Step 6: Commit Task 1**

Run:

```bash
git add go/download.go go/download_test.go
git commit -m "feat: resolve vialite release artifacts by platform"
```

## Task 2: Native Build Artifact Staging

**Files:**
- Modify: `build/Dockerfile`
- Modify: `go/assets/README.md`

- [ ] **Step 1: Update Dockerfile to stage the subprocess binary**

Replace the `FROM build AS shared` section in `build/Dockerfile` with:

```dockerfile
FROM build AS shared
WORKDIR /out
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/libvialite.so /out/libvialite-${TARGETOS}-${TARGETARCH}.so
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/libvialite.h /out/libvialite.h
RUN cp /src/build/.work/ViaProxy/vialite-native/build/native/nativeCompile/vialite /out/vialite-${TARGETOS}-${TARGETARCH}
```

- [ ] **Step 2: Update asset README**

Replace `go/assets/README.md` with:

```markdown
# Embedded vialite Assets

Per-arch native artifacts used by the Go package. Linux artifacts can be
embedded into the host binary with `-tags vialite_embed` after fetching or
copying release assets into this directory.

Expected Linux embed layout:

- `vialite-linux-amd64`
- `libvialite-linux-amd64.so`
- `vialite-linux-arm64`
- `libvialite-linux-arm64.so`

GitHub Release auto-download supports:

- `libvialite-linux-amd64.so`
- `libvialite-linux-arm64.so`
- `vialite-linux-amd64`
- `vialite-linux-arm64`
- `vialite-windows-amd64.exe`
- `checksums.txt`

Windows uses subprocess mode with the released `.exe`; Windows embedded DLL
support is intentionally not shipped yet.
```

- [ ] **Step 3: Run local non-Docker checks**

Run:

```bash
go -C go test ./...
```

Expected: all Go tests pass. The Docker native build is not expected to run on this machine unless Docker/OrbStack is available.

- [ ] **Step 4: Commit Task 2**

Run:

```bash
git add build/Dockerfile go/assets/README.md
git commit -m "build: stage vialite native binary artifacts"
```

## Task 3: GitHub Native Build Matrix

**Files:**
- Modify: `.github/workflows/native-image.yml`

- [ ] **Step 1: Replace single Linux job with matrix**

Edit `.github/workflows/native-image.yml` so the Linux build job uses:

```yaml
jobs:
  build:
    runs-on: ${{ matrix.runner }}
    timeout-minutes: 60
    strategy:
      fail-fast: false
      matrix:
        include:
          - arch: amd64
            runner: ubuntu-latest
          - arch: arm64
            runner: ubuntu-24.04-arm
```

The build command must pass `TARGETARCH=${{ matrix.arch }}` and the extraction check must assert:

```bash
test -f artifacts/libvialite-linux-${{ matrix.arch }}.so
test -f artifacts/vialite-linux-${{ matrix.arch }}
grep -E 'vialite_init.*graal_isolatethread_t' artifacts/libvialite.h
```

The upload artifact name must be:

```yaml
name: vialite-linux-${{ matrix.arch }}
```

- [ ] **Step 2: Add Windows executable job**

Add a `build-windows` job to `.github/workflows/native-image.yml` that checks out the repo, sets up GraalVM, applies the ViaProxy overlay, builds the native executable with `native-image`, stages `artifacts/vialite-windows-amd64.exe`, and uploads artifact `vialite-windows-amd64`.

Use this skeleton:

```yaml
  build-windows:
    runs-on: windows-latest
    timeout-minutes: 60
    steps:
      - uses: actions/checkout@v6
      - uses: graalvm/setup-graalvm@v1
        with:
          java-version: '21'
          distribution: 'graalvm-community'
          github-token: ${{ secrets.GITHUB_TOKEN }}
      - name: Apply ViaProxy overlay
        shell: bash
        run: bash build/apply-overlay.sh
      - name: Build Windows executable
        shell: pwsh
        run: |
          Set-Location build/.work/ViaProxy
          ./gradlew.bat :vialite-native:nativeCompile --no-daemon --no-configuration-cache
          New-Item -ItemType Directory -Force ../../../artifacts | Out-Null
          Copy-Item vialite-native/build/native/nativeCompile/vialite.exe ../../../artifacts/vialite-windows-amd64.exe
      - uses: actions/upload-artifact@v7
        with:
          name: vialite-windows-amd64
          path: artifacts/
          if-no-files-found: error
```

- [ ] **Step 3: Run YAML lint if available**

Run:

```bash
yamllint .github/workflows/native-image.yml
```

Expected: no YAML syntax or lint errors. If `yamllint` is unavailable, run `ruby -e 'require "yaml"; YAML.load_file(".github/workflows/native-image.yml")'` and record that only YAML parsing was verified locally.

- [ ] **Step 4: Commit Task 3**

Run:

```bash
git add .github/workflows/native-image.yml
git commit -m "ci: build vialite native artifacts per platform"
```

## Task 4: Release Workflow Collection

**Files:**
- Modify: `.github/workflows/release.yml`

- [ ] **Step 1: Replace release-time rebuild with artifact collection**

Update `.github/workflows/release.yml` to:

1. Add `actions: read` permission.
2. Remove the release-time Docker build step.
3. Add a step that finds the latest successful `native-image.yml` run for the native source commit.
4. Download artifacts with pattern `vialite-*` into `artifacts/`.

The native source commit lookup should be:

```bash
NATIVE_SHA=$(git log -n 1 --format=%H -- \
  build \
  .github/workflows/native-image.yml)
```

The download command should be:

```bash
gh run download "$RUN_ID" --dir artifacts --pattern 'vialite-*'
```

- [ ] **Step 2: Keep checksum generation but exclude signatures**

Ensure the checksum step still writes basenames only:

```bash
( cd artifacts
  shopt -s globstar nullglob
  for f in **/*; do
    [ -f "$f" ] || continue
    base="$(basename "$f")"
    case "$base" in *.sig|*.attest.spdx.json|checksums.txt) continue;; esac
    sha256sum "$f" | awk -v b="$base" '{ print $1 "  " b }'
  done | sort -k2 > /tmp/checksums.txt
)
mv /tmp/checksums.txt artifacts/checksums.txt
```

- [ ] **Step 3: Parse workflow YAML**

Run:

```bash
ruby -e 'require "yaml"; YAML.load_file(".github/workflows/release.yml")'
```

Expected: command exits 0.

- [ ] **Step 4: Commit Task 4**

Run:

```bash
git add .github/workflows/release.yml
git commit -m "ci: release vialite native artifact matrix"
```

## Task 5: Documentation And Verification

**Files:**
- Modify: `README.md`

- [ ] **Step 1: Update README artifact status**

In `README.md`, update the `Release/update loop` component row or nearby status text to say release assets cover Linux amd64/arm64 shared libraries and subprocess binaries plus Windows amd64 subprocess binary. Keep the existing caveat that full Via translation is a later implementation slice.

- [ ] **Step 2: Run verification**

Run:

```bash
go -C go test ./...
yamllint .
markdownlint-cli2 '**/*.md' '#build/.work/**'
```

Expected: Go tests pass. YAML and Markdown lint pass if the tools are installed. If a lint tool is missing, record the missing tool and run YAML parsing for changed workflow files.

- [ ] **Step 3: Inspect final diff**

Run:

```bash
git diff --stat HEAD~4..HEAD
git status --short --branch
```

Expected: only intended files are changed or committed; working tree is clean before PR/merge.

- [ ] **Step 4: Commit documentation if needed**

Run:

```bash
git add README.md
git commit -m "docs: document vialite artifact matrix"
```

Skip this commit if `README.md` did not need changes.
