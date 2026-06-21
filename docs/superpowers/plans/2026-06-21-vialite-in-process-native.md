# vialite In-Process Native Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the initial `minekube/vialite` repository: a Geyserlite-shaped Go library and Gate adapter for an in-process native Via backend compatibility layer, with native ABI/build scaffolding, release automation, docs, and tests.

**Architecture:** The first milestone builds the Go API and lifecycle around an embedded `libvialite.so` runtime, with subprocess fallback and content-addressed artifact lookup. The native layer is represented by a GraalVM overlay scaffold and explicit C ABI contract; full Via translation internals are isolated behind that ABI so Gate can integrate through `BackendDialAddress` first and direct handoff later.

**Tech Stack:** Go 1.26, purego, GraalVM native-image, ViaProxy/ViaVersion upstream source overlay, GitHub Actions, Renovate, release-please, go-task, mise.

---

## File Structure

- `README.md`: product overview, topology, quick start, repo layout, release/update loop.
- `docs/architecture.md`: detailed in-process native architecture and Gate data flow.
- `docs/gate.md`: Gate integration model and config shape.
- `docs/troubleshooting.md`: artifact lookup, embedded/subprocess behavior, backend detection failures.
- `go/go.mod`: Go module `go.minekube.com/vialite`.
- `go/doc.go`: package documentation.
- `go/vialite.go`: public enums, options, backend config, sentinel errors.
- `go/options.go`: defaults and validation.
- `go/server.go`: `Server` lifecycle and runner abstraction.
- `go/embedded.go`, `go/embedded_helpers.go`, `go/library_unix.go`, `go/library_windows.go`, `go/library_unsupported.go`: shared-library runner and symbol loading.
- `go/subprocess.go`, `go/subprocess_unix.go`, `go/subprocess_other.go`: subprocess fallback runner.
- `go/download.go`, `go/locate.go`, `go/embed*.go`, `go/version.go`: artifact resolution, checksum verification, embed support, default release metadata.
- `go/config.go`: JSON config sent to native `vialite_init`.
- `go/integration/gate/config.go`, `go/integration/gate/gate.go`: Gate-shaped adapter.
- `build/via.version`, `build/graalvm.version`, `build/apply-overlay.sh`, `build/Dockerfile`, `build/overlay/vialite-native/build.gradle.kts`, `build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteBridge.java`: upstream source pin, overlay, and native ABI scaffold.
- `.github/workflows/ci.yml`, `.github/workflows/native-image.yml`, `.github/workflows/release.yml`, `.github/workflows/release-please.yml`: CI and release automation.
- `Taskfile.yml`, `mise.toml`, `renovate.json`, `.release-please-config.json`, `.release-please-manifest.json`: local and automated developer workflows.

---

### Task 1: Repository Baseline

**Files:**

- Create: `README.md`
- Create: `docs/architecture.md`
- Create: `docs/gate.md`
- Create: `docs/troubleshooting.md`
- Create: `mise.toml`
- Create: `Taskfile.yml`
- Create: `.gitignore`
- Create: `.markdownlint.yaml`
- Create: `.yamllint`

- [ ] **Step 1: Add baseline docs and tooling files**

Create the files listed above with the project overview, in-process topology, Gate backend flow, local tool pins, and task targets.

- [ ] **Step 2: Verify markdown and YAML files exist**

Run:

```bash
test -f README.md
test -f docs/architecture.md
test -f docs/gate.md
test -f docs/troubleshooting.md
test -f mise.toml
test -f Taskfile.yml
```

Expected: exit 0.

- [ ] **Step 3: Commit baseline**

```bash
git add README.md docs mise.toml Taskfile.yml .gitignore .markdownlint.yaml .yamllint
git commit -m "chore: add repository baseline"
```

---

### Task 2: Go Module Public API

**Files:**

- Create: `go/go.mod`
- Create: `go/doc.go`
- Create: `go/vialite.go`
- Create: `go/options.go`
- Create: `go/config.go`
- Create: `go/version.go`
- Create: `go/options_test.go`
- Create: `go/config_test.go`

- [ ] **Step 1: Write tests for options and native config**

Tests must cover default mode, default bind, default logger, restart defaults, missing backend name, duplicate backend names, invalid backend address, forwarding validation, and JSON config conversion.

- [ ] **Step 2: Run tests and verify they fail before implementation**

Run:

```bash
cd go && go test ./...
```

Expected: fails because the package files are not implemented yet.

- [ ] **Step 3: Implement public API and validation**

Implement `Mode`, `ForwardingMode`, `Backend`, `Options`, `RestartPolicy`, sentinel errors, `Options.validate`, and native config JSON construction.

- [ ] **Step 4: Run tests**

Run:

```bash
cd go && go test ./...
```

Expected: pass.

- [ ] **Step 5: Commit Go API**

```bash
git add go
git commit -m "feat: add vialite Go API"
```

---

### Task 3: Artifact Lookup, Download, And Embed Support

**Files:**

- Create: `go/download.go`
- Create: `go/download_test.go`
- Create: `go/locate.go`
- Create: `go/locate_test.go`
- Create: `go/embed.go`
- Create: `go/embed_default.go`
- Create: `go/embed_linux_amd64.go`
- Create: `go/embed_linux_arm64.go`
- Create: `go/embed_unsupported.go`
- Create: `go/assets/README.md`

- [ ] **Step 1: Write artifact tests**

Tests must cover Linux amd64 and arm64 asset names, unsupported OS/arch errors, checksum parsing, content-addressed cache path, explicit path lookup, environment variable lookup, offline mode, and embed fallback.

- [ ] **Step 2: Run tests and verify they fail before implementation**

Run:

```bash
cd go && go test ./...
```

Expected: fails because artifact functions are missing.

- [ ] **Step 3: Implement artifact resolution**

Implement `assetFor`, `downloadAsset`, `fetchExpectedSha`, `verifiedDownloadPath`, `locateBinary`, `locateLibrary`, and embed extraction stubs.

- [ ] **Step 4: Run tests**

Run:

```bash
cd go && go test ./...
```

Expected: pass.

- [ ] **Step 5: Commit artifact layer**

```bash
git add go
git commit -m "feat: add native artifact resolution"
```

---

### Task 4: Server Lifecycle And Runners

**Files:**

- Create: `go/server.go`
- Create: `go/server_test.go`
- Create: `go/embedded.go`
- Create: `go/embedded_helpers.go`
- Create: `go/library_unix.go`
- Create: `go/library_windows.go`
- Create: `go/library_unsupported.go`
- Create: `go/subprocess.go`
- Create: `go/subprocess_unix.go`
- Create: `go/subprocess_other.go`
- Create: `go/subprocess_test.go`

- [ ] **Step 1: Write lifecycle tests**

Tests must cover `New`, double start rejection, stop before start, graceful
cancellation, `WaitReady`, `Healthy`, `BackendDialAddress`, embedded runner
fake behavior, subprocess command construction with a fake executable
script, and restart policy behavior.

- [ ] **Step 2: Run tests and verify they fail before implementation**

Run:

```bash
cd go && go test ./...
```

Expected: fails because lifecycle files are missing.

- [ ] **Step 3: Implement server and runners**

Implement `Server`, `runner`, readiness publication, embedded runner symbol
registration, Graal isolate lifecycle, subprocess runner restart loop, and
backend address lookup.

- [ ] **Step 4: Run tests**

Run:

```bash
cd go && go test ./...
```

Expected: pass.

- [ ] **Step 5: Commit lifecycle layer**

```bash
git add go
git commit -m "feat: add vialite server lifecycle"
```

---

### Task 5: Gate Adapter

**Files:**

- Create: `go/integration/gate/config.go`
- Create: `go/integration/gate/config_test.go`
- Create: `go/integration/gate/gate.go`
- Create: `go/integration/gate/gate_test.go`

- [ ] **Step 1: Write adapter tests**

Tests must cover disabled config returns nil, backend conversion, mode
conversion, forwarding conversion, invalid mode, invalid forwarding mode,
fake server lifecycle, readiness, and `BackendDialAddress`.

- [ ] **Step 2: Run tests and verify they fail before implementation**

Run:

```bash
cd go && go test ./...
```

Expected: fails because adapter files are missing.

- [ ] **Step 3: Implement Gate adapter**

Implement `Config`, `BackendConfig`, `toOptions`, `Via`, `New`, `Start`,
`WaitReady`, `Stop`, `Healthy`, and `BackendDialAddress`.

- [ ] **Step 4: Run tests**

Run:

```bash
cd go && go test ./...
```

Expected: pass.

- [ ] **Step 5: Commit adapter**

```bash
git add go/integration/gate
git commit -m "feat: add Gate adapter"
```

---

### Task 6: Native Build Overlay Scaffold

**Files:**

- Create: `build/via.version`
- Create: `build/graalvm.version`
- Create: `build/apply-overlay.sh`
- Create: `build/Dockerfile`
- Create: `build/README.md`
- Create: `build/overlay/README.md`
- Create: `build/overlay/vialite-native/build.gradle.kts`
- Create: `build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteBridge.java`
- Create: `build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteBridgeFeature.java`
- Create: `build/agent-config/README.md`

- [ ] **Step 1: Add native scaffold**

Create a soft-fork overlay that clones ViaProxy at `build/via.version`, adds a `vialite-native` Gradle subproject, and defines the C ABI entry points.

- [ ] **Step 2: Verify shell script syntax**

Run:

```bash
bash -n build/apply-overlay.sh
```

Expected: pass.

- [ ] **Step 3: Verify expected ABI names are present**

Run:

```bash
rg "vialite_init|vialite_run|vialite_shutdown|vialite_status|vialite_backend_address" build/overlay/vialite-native
```

Expected: all five symbols appear.

- [ ] **Step 4: Commit native scaffold**

```bash
git add build
git commit -m "build: add native Via overlay scaffold"
```

---

### Task 7: CI And Release Automation

**Files:**

- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/native-image.yml`
- Create: `.github/workflows/release.yml`
- Create: `.github/workflows/release-please.yml`
- Create: `.release-please-config.json`
- Create: `.release-please-manifest.json`
- Create: `renovate.json`

- [ ] **Step 1: Add automation files**

Create CI for Go tests and docs lint, native-image workflow scaffold, release workflow with checksums, and release-please metadata.

- [ ] **Step 2: Validate workflow files parse as YAML**

Run:

```bash
python - <<'PY'
from pathlib import Path
import yaml
for path in Path('.github/workflows').glob('*.yml'):
    yaml.safe_load(path.read_text())
    print(path)
PY
```

Expected: prints all workflow files and exits 0.

- [ ] **Step 3: Commit automation**

```bash
git add .github .release-please-config.json .release-please-manifest.json renovate.json
git commit -m "ci: add vialite automation"
```

---

### Task 8: Final Verification

**Files:**

- Modify as needed based on verification failures.

- [ ] **Step 1: Run Go tests**

Run:

```bash
cd go && go test ./...
```

Expected: pass.

- [ ] **Step 2: Run repository checks**

Run:

```bash
bash -n build/apply-overlay.sh
python - <<'PY'
from pathlib import Path
import yaml
for path in Path('.github/workflows').glob('*.yml'):
    yaml.safe_load(path.read_text())
print('workflow yaml ok')
PY
```

Expected: pass.

- [ ] **Step 3: Check git status**

Run:

```bash
git status --short
```

Expected: clean.

- [ ] **Step 4: Push final branch**

Run:

```bash
git push
```

Expected: remote branch updated.
