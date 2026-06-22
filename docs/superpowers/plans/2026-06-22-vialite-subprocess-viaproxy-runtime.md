# vialite Subprocess ViaProxy Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the subprocess scaffold with a real ViaProxy-backed native runtime that Gate can dial per backend.

**Architecture:** The Go subprocess runner splits configured backends into one native child process per backend and publishes those child listener addresses to Gate. The native executable reads a one-backend JSON config, writes an equivalent ViaProxy YAML config, and starts ViaProxy headlessly so Via's packet pipeline handles translation. Embedded mode remains a scaffold and is not used for compatibility testing in this slice.

**Tech Stack:** Go subprocess lifecycle, JSON config, Java 21, ViaProxy Netty/Via pipeline, GraalVM native-image, GitHub release artifacts.

---

## File Structure

- Modify `go/config.go`: add helper to produce a one-backend native config without changing the public `Options` API.
- Modify `go/subprocess.go`: manage a process group, one child per backend, and publish per-backend listener addresses.
- Modify `go/subprocess_test.go`: add red tests for multiple backend child processes and early child failure.
- Modify `build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteBridge.java`: parse `--config`, validate one backend, generate ViaProxy YAML, start ViaProxy CLI config mode, and keep the process alive.
- Create `build/overlay/vialite-native/src/test/java/com/minekube/vialite/bridge/VialiteBridgeConfigTest.java`: unit-test config parsing and YAML generation.
- Modify `build/overlay/vialite-native/build.gradle.kts`: add JUnit test dependencies and configure tests.

---

## Task 1: Go Per-Backend Subprocess Split

**Files:**
- Modify: `go/config.go`
- Modify: `go/subprocess.go`
- Modify: `go/subprocess_test.go`

- [ ] **Step 1: Add failing multiple-backend subprocess test**

Add this test to `go/subprocess_test.go`:

```go
func TestSubprocessRunnerStartsOneProcessPerBackend(t *testing.T) {
	dir := t.TempDir()
	bin := buildSubprocessHelper(t)
	t.Setenv("VIALITE_HELPER_RECORD_DIR", dir)

	opts, err := Options{
		Mode:            ModeSubprocess,
		BinaryPath:      bin,
		ShutdownTimeout: time.Second,
		Backends: []Backend{
			{Name: "old", Address: "127.0.0.1:25566"},
			{Name: "new", Address: "127.0.0.1:25567"},
		},
	}.validate()
	if err != nil {
		t.Fatalf("validate: %v", err)
	}

	srv := &Server{opts: opts, runner: &subprocessRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()

	if err := srv.WaitReady(context.Background()); err != nil {
		cancel()
		t.Fatalf("WaitReady: %v", err)
	}
	oldAddr, err := srv.BackendDialAddress("old")
	if err != nil {
		cancel()
		t.Fatalf("old BackendDialAddress: %v", err)
	}
	newAddr, err := srv.BackendDialAddress("new")
	if err != nil {
		cancel()
		t.Fatalf("new BackendDialAddress: %v", err)
	}
	if oldAddr == newAddr {
		cancel()
		t.Fatalf("backend addresses are equal: %s", oldAddr)
	}
	for _, addr := range []string{oldAddr, newAddr} {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err != nil {
			cancel()
			t.Fatalf("dial backend listener %s: %v", addr, err)
		}
		_ = conn.Close()
	}
	if got := countFiles(t, dir, "config-*.json"); got != 2 {
		cancel()
		t.Fatalf("started configs = %d, want 2", got)
	}
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Start returned %v, want nil", err)
	}
	cancel()
}
```

Add this helper to the same file:

```go
func countFiles(t *testing.T, dir, pattern string) int {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, pattern))
	if err != nil {
		t.Fatal(err)
	}
	return len(matches)
}
```

- [ ] **Step 2: Extend test helper to record per-process configs**

In `subprocessHelperSource`, after reading `data`, add:

```go
if dir := os.Getenv("VIALITE_HELPER_RECORD_DIR"); dir != "" {
	entries, _ := os.ReadDir(dir)
	name := filepath.Join(dir, fmt.Sprintf("config-%d.json", len(entries)+1))
	_ = os.WriteFile(name, data, 0o644)
}
```

Add `fmt` and `path/filepath` to the helper imports.

- [ ] **Step 3: Run red test**

Run:

```bash
cd go
go test ./... -run TestSubprocessRunnerStartsOneProcessPerBackend -count=1
```

Expected: fail because the current runner starts one child process for all backends.

- [ ] **Step 4: Add one-backend config helper**

In `go/config.go`, add:

```go
func (o Options) nativeConfigJSONForBackend(backend Backend, bind string) ([]byte, error) {
	cfg := nativeConfig{
		Bind:         bind,
		GateProtocol: o.GateProtocol,
		Backends: []nativeBackendConfig{{
			Name:       backend.Name,
			Address:    backend.Address,
			Version:    backend.Version,
			Detect:     backend.Detect,
			Forwarding: string(backend.Forwarding),
		}},
	}
	return json.Marshal(cfg)
}
```

- [ ] **Step 5: Replace single subprocess state with child group**

In `go/subprocess.go`, replace the `cmd *exec.Cmd` field with:

```go
children map[string]*subprocessChild
```

Add:

```go
type subprocessChild struct {
	name       string
	addr       string
	configPath string
	cmd        *exec.Cmd
	done       chan error
}
```

- [ ] **Step 6: Implement per-backend child start**

Add a helper:

```go
func (r *subprocessRunner) startChildren(ctx context.Context, bin string, opts Options) (map[string]*subprocessChild, map[string]string, error) {
	children := make(map[string]*subprocessChild, len(opts.Backends))
	backends := make(map[string]string, len(opts.Backends))
	for i, backend := range opts.Backends {
		addr, err := loopbackBackendAddress(opts.Bind, i)
		if err != nil {
			return nil, nil, fmt.Errorf("vialite: allocate subprocess backend %s: %w", backend.Name, err)
		}
		config, err := opts.nativeConfigJSONForBackend(backend, addr)
		if err != nil {
			return nil, nil, err
		}
		configPath, err := writeTempConfig(config)
		if err != nil {
			return nil, nil, err
		}
		cmd := exec.CommandContext(ctx, bin, "--config", configPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		child := &subprocessChild{
			name: backend.Name, addr: addr, configPath: configPath,
			cmd: cmd, done: make(chan error, 1),
		}
		if err := cmd.Start(); err != nil {
			_ = os.Remove(configPath)
			return nil, nil, err
		}
		go func() { child.done <- cmd.Wait() }()
		children[backend.Name] = child
		backends[backend.Name] = addr
	}
	return children, backends, nil
}
```

- [ ] **Step 7: Implement group readiness and shutdown**

Add helpers:

```go
func waitChildListeners(ctx context.Context, children map[string]*subprocessChild) error {
	backends := make(map[string]string, len(children))
	done := make(chan error, len(children))
	for name, child := range children {
		backends[name] = child.addr
		go func(c *subprocessChild) {
			if err := <-c.done; err != nil {
				done <- fmt.Errorf("vialite: subprocess backend %s exited: %w", c.name, err)
				return
			}
			done <- fmt.Errorf("vialite: subprocess backend %s exited", c.name)
		}(child)
	}
	return waitBackendListeners(ctx, done, backends)
}
```

Add:

```go
func stopChildren(children map[string]*subprocessChild, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for _, child := range children {
		if child.cmd != nil && child.cmd.Process != nil && child.cmd.ProcessState == nil {
			_ = terminateProcess(child.cmd.Process)
		}
	}
	for _, child := range children {
		select {
		case <-child.done:
		case <-ctx.Done():
			if child.cmd != nil && child.cmd.Process != nil {
				_ = child.cmd.Process.Kill()
			}
		}
		_ = os.Remove(child.configPath)
	}
}
```

- [ ] **Step 8: Update `run` to restart the child group**

Rewrite `subprocessRunner.run` so each restart attempt calls `startChildren`,
stores `children` and `backends`, waits for all listeners, marks ready, then
waits until context cancellation or any child exits. On any child exit after
readiness, stop the remaining children and let the existing restart policy
retry the whole group.

- [ ] **Step 9: Run Go tests**

Run:

```bash
cd go
go test ./...
```

Expected: pass.

- [ ] **Step 10: Commit Go runtime split**

```bash
git add go/config.go go/subprocess.go go/subprocess_test.go
git commit -m "feat: run one vialite subprocess per backend"
```

---

## Task 2: Java Native Config and ViaProxy CLI Runtime

**Files:**
- Modify: `build/overlay/vialite-native/build.gradle.kts`
- Modify: `build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteBridge.java`
- Create: `build/overlay/vialite-native/src/test/java/com/minekube/vialite/bridge/VialiteBridgeConfigTest.java`

- [ ] **Step 1: Add Java tests and Gradle test support**

In `build/overlay/vialite-native/build.gradle.kts`, add:

```kotlin
dependencies {
    compileOnly("org.graalvm.sdk:nativeimage:25.0.3")
    compileOnly(rootProject)
    testImplementation(rootProject)
    testImplementation(platform("org.junit:junit-bom:5.11.4"))
    testImplementation("org.junit.jupiter:junit-jupiter")
}

tasks.test {
    useJUnitPlatform()
}
```

Create `VialiteBridgeConfigTest.java` with tests that call package-visible
helpers:

```java
package com.minekube.vialite.bridge;

import static org.junit.jupiter.api.Assertions.*;

import org.junit.jupiter.api.Test;

final class VialiteBridgeConfigTest {
    @Test
    void rejectsMultipleBackends() {
        String json = """
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"a","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"none"},
              {"name":"b","address":"127.0.0.1:25567","version":"auto","detect":true,"forwarding":"none"}
            ]}
            """;
        IllegalArgumentException ex = assertThrows(IllegalArgumentException.class,
            () -> VialiteBridge.parseConfig(json));
        assertTrue(ex.getMessage().contains("exactly one backend"));
    }

    @Test
    void writesLegacyForwardingViaProxyYaml() {
        VialiteBridge.NativeConfig config = VialiteBridge.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"legacy"}
            ]}
            """);
        String yaml = VialiteBridge.toViaProxyYaml(config);
        assertTrue(yaml.contains("bind-address: 127.0.0.1:25590"));
        assertTrue(yaml.contains("target-address: 127.0.0.1:25566"));
        assertTrue(yaml.contains("target-version: auto"));
        assertTrue(yaml.contains("bungeecord-player-info-passthrough: true"));
    }

    @Test
    void rejectsVelocityForwardingUntilProven() {
        VialiteBridge.NativeConfig config = VialiteBridge.parseConfig("""
            {"bind":"127.0.0.1:25590","backends":[
              {"name":"lobby","address":"127.0.0.1:25566","version":"auto","detect":true,"forwarding":"velocity"}
            ]}
            """);
        IllegalArgumentException ex = assertThrows(IllegalArgumentException.class,
            () -> VialiteBridge.toViaProxyYaml(config));
        assertTrue(ex.getMessage().contains("velocity"));
    }
}
```

- [ ] **Step 2: Run Java red tests**

Run:

```bash
bash build/apply-overlay.sh
cd build/.work/ViaProxy
./gradlew :vialite-native:test --no-daemon --no-configuration-cache
```

Expected: fail because helpers do not exist yet.

- [ ] **Step 3: Implement config parser and YAML writer**

In `VialiteBridge.java`, add package-visible static classes:

```java
static final class NativeConfig {
    String bind;
    List<NativeBackend> backends;
}

static final class NativeBackend {
    String name;
    String address;
    String version;
    boolean detect;
    String forwarding;
}
```

Add:

```java
static NativeConfig parseConfig(String json) {
    NativeConfig config = GSON.fromJson(json, NativeConfig.class);
    if (config == null || config.backends == null || config.backends.size() != 1) {
        throw new IllegalArgumentException("vialite native config must contain exactly one backend");
    }
    if (config.bind == null || config.bind.isBlank()) {
        config.bind = "127.0.0.1:0";
    }
    NativeBackend backend = config.backends.getFirst();
    if (backend.name == null || backend.name.isBlank()) {
        throw new IllegalArgumentException("backend name is required");
    }
    if (backend.address == null || backend.address.isBlank()) {
        throw new IllegalArgumentException("backend address is required");
    }
    if (backend.version == null || backend.version.isBlank() || backend.detect) {
        backend.version = "auto";
    }
    if (backend.forwarding == null || backend.forwarding.isBlank()) {
        backend.forwarding = "none";
    }
    return config;
}
```

Add `toViaProxyYaml(NativeConfig config)` that writes:

```yaml
bind-address: <bind>
target-address: <backend.address>
target-version: <backend.version>
proxy-online-mode: false
auth-method: none
bungeecord-player-info-passthrough: <true for legacy>
rewrite-handshake-packet: true
rewrite-transfer-packets: true
```

If forwarding is `velocity`, throw `IllegalArgumentException("velocity forwarding is not supported by this vialite runtime slice")`.

- [ ] **Step 4: Implement executable main**

Replace the scaffold `main` with:

```java
public static void main(String[] args) throws Exception {
    File configPath = configPath(args);
    NativeConfig config = parseConfig(Files.readString(configPath.toPath()));
    Path cwd = Files.createTempDirectory("vialite-viaproxy-");
    Files.writeString(cwd.resolve("viaproxy.yml"), toViaProxyYaml(config));
    System.setProperty("skipUpdateCheck", "true");
    System.setProperty("java.awt.headless", "true");
    ViaProxy.main(new String[]{"config", cwd.resolve("viaproxy.yml").toString()});
}
```

Add `configPath(String[] args)` that requires `--config <path>`.

- [ ] **Step 5: Keep C ABI scaffold explicit**

Leave `vialite_init`, `vialite_run`, `vialite_shutdown`, `vialite_status`, and
`vialite_backend_address` compiling, but do not claim embedded mode is real.
The subprocess runtime uses `main`.

- [ ] **Step 6: Run Java tests and overlay compile check**

Run:

```bash
bash build/apply-overlay.sh
cd build/.work/ViaProxy
./gradlew :vialite-native:test :vialite-native:tasks --no-daemon --no-configuration-cache
```

Expected: pass.

- [ ] **Step 7: Commit Java runtime entrypoint**

```bash
git add build/overlay/vialite-native/build.gradle.kts build/overlay/vialite-native/src/main/java/com/minekube/vialite/bridge/VialiteBridge.java build/overlay/vialite-native/src/test/java/com/minekube/vialite/bridge/VialiteBridgeConfigTest.java
git commit -m "feat: start viaproxy from vialite subprocess"
```

---

## Task 3: Release and Compatibility Verification

**Files:**
- Modify only if needed after tests reveal failures.

- [ ] **Step 1: Run repository checks**

Run:

```bash
task test
mise exec -- task lint
```

- [ ] **Step 2: Build native artifacts in CI**

Push the branch and open a PR. Wait for:

- Linux amd64 native build
- Linux arm64 native build
- Windows amd64 native build
- Go tests and docs lint

- [ ] **Step 3: Merge and release**

After CI and review, merge the vialite PR. Create the next release tag, verify
release assets, and verify `sha256sum -c checksums.txt`.

- [ ] **Step 4: Update Gate**

Update Gate to the new `go.minekube.com/vialite` release and verify:

```bash
go list -m all | rg 'go\.minekube\.com/vialite|github\.com/minekube/vialite'
go mod verify
go test ./...
```

- [ ] **Step 5: Runtime matrix**

Run Gate in classic mode with:

```yaml
config:
  onlineMode: false
  servers:
    old: 127.0.0.1:<old-paper-port>
    new: 127.0.0.1:<new-paper-port>
  try:
    - old
  forwarding:
    mode: none
  via:
    enabled: true
    mode: subprocess
```

Verify:

- a newer client can join an older Paper backend through Gate and vialite;
- an older client can join a newer Paper backend through Gate and vialite, if
  the Via stack supports that path;
- Gate logs show vialite backend dial addresses, not direct Paper addresses.
