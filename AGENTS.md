# Agent Notes

This repository packages Via-powered Java protocol compatibility for Gate.
Future agents should keep the integration model and upstream state clear before
changing code or docs.

## Live Checks

Do not rely on cached knowledge for Minecraft, ViaProxy, ViaVersion, Gate, or
release state. Check live sources when a task involves version support,
upstream compatibility, releases, CI, or repository metadata.

Useful checks:

```sh
gh repo view minekube/vialite --json defaultBranchRef,homepageUrl,url
gh release view --repo minekube/vialite --json tagName,publishedAt,url,assets
gh api repos/ViaVersion/ViaProxy/commits/master --jq '{sha:.sha,date:.commit.committer.date,message:.commit.message}'
gh api repos/ViaVersion/ViaVersion/releases/latest --jq '{tag:.tag_name,url:.html_url,published_at}'
```

When Minecraft protocol support is discussed, also verify the relevant Mojang
release notes and ViaVersion/ViaProxy project state before making claims.

## Architecture Rules

`vialite` is backend-side protocol translation for Gate classic:

```text
Java player
  -> Gate classic
  -> vialite
  -> backend server

Bedrock player
  -> optional geyserlite
  -> Gate classic
  -> vialite
  -> backend server
```

It is not a Gate Lite feature. Lite intentionally raw-pipes after the initial
handshake so backend servers keep authentication ownership. Via translation must
decode and rewrite packets after Gate has accepted the player and selected a
backend.

`vialite` can help early adopters when a Java backend has moved to a newer
Minecraft protocol and Via can translate between Gate's backend-facing protocol
and that backend. It does not fix unsupported Bedrock client protocols or
Geyser Bedrock-to-Java translation gaps.

## Agent Workflow

Use an isolated worktree for feature work. For non-trivial changes, write down
the implementation plan before editing. For bugs or compatibility failures,
debug from evidence: reproduce, inspect logs, identify the failing boundary, and
then change code. Before opening or merging a PR, run fresh verification and get
a code review from a subagent or another reviewer when available.

Relevant workflow skills, when the agent runtime provides them:

- `superpowers:using-git-worktrees`
- `superpowers:systematic-debugging`
- `superpowers:writing-plans`
- `superpowers:verification-before-completion`
- `superpowers:requesting-code-review`

## Update Policy

- `build/via.version` pins the upstream ViaProxy source ref used by the native
  overlay.
- Runtime artifact resolution (`go/download.go`): an empty/unset `Version`
  means "latest", exactly like `auto`/`latest`. With a `Mirror` set, the
  mirror's own `/latest` is asked first; only a mirror that cannot answer falls
  back to `DefaultMirrorVersion`, and that fallback logs a warning naming the
  version and the remedy. `DefaultMirrorVersion` is owned by release-please, not
  by hand: `.release-please-config.json` declares `go/version.go` under
  `extra-files` and the constant carries the `x-release-please-version`
  annotation, so the release pull request bumps it to the released `vX.Y.Z`
  alongside `.release-please-manifest.json`; `go/version_test.go` fails when that
  wiring or the manifest agreement breaks, and the daily workflow still fails
  closed when the constant trails by more than one release. Never hand-edit the
  version and never move that annotation.
  Never make an explicit `auto`/`latest` silently downgrade.
- Every start logs one `vialite: resolved runtime` line with the artifact
  version and provenance (download/cache/binaryPath/env/embedded/path/library
  source). Keep that line when touching `go/locate.go` or `go/download.go`:
  support uses it to tell which runtime (and therefore which ViaVersion
  ceiling) an operator is running.
- `.github/workflows/bump-upstream-pin.yml` is the periodic upstream update:
  daily, and on manual dispatch, it resolves the latest upstream ViaProxy
  release, reports the bundled ViaVersion against the latest upstream release,
  pushes `automation/bump-viaproxy` and opens a reviewed pull request with CI
  and the native image build (optionally the Craftless real-client smoke)
  attached. It never merges, and it dispatches those workflows itself because
  pulls opened with `GITHUB_TOKEN` do not trigger `pull_request` runs.
- The merge -> release -> downstream chain and its remaining manual steps
  (overlay re-derivation, Gate's Go module bump while
  `RELEASE_CASCADE_APP_PRIVATE_KEY` is stale) are in
  [`docs/release-runbook.md`](docs/release-runbook.md). Read it before cutting a
  release.
- Renovate (`renovate.json`) is not installed for this repository and opens no
  pull requests here; geyserlite gets its upstream Geyser bumps from Renovate.
  If Renovate is enabled later, keep it from racing the workflow above over
  `build/via.version`.
- Agents must still inspect the upstream diff and the attached validation runs
  before assuming a bump is safe.
- Releases publish checksummed native artifacts. Gate consumes those releases
  through its managed dependency update workflow.
- Keep release-chain changes explicit: ViaLite release -> Gate managed
  dependency bump -> Gate release -> downstream consumers.

## Development Checks

Start with:

```sh
mise trust
mise install
mise run setup
```

Common checks:

```sh
mise run test
mise run lint
mise run overlay:apply
```

Use `mise run build:native` only when native-image behavior is relevant. It is
slower and Docker-backed.

Before merging code changes, verify at least the affected Go tests and linting.
For native overlay or protocol-routing changes, also run `mise run overlay:apply`
and check the relevant CI jobs.

### Timing in the subprocess tests

The subprocess tests fork a real helper binary and wait for its loopback
listener, so their duration is the pod's scheduling latency, not the code's.
Measured on the 2-CPU Hermes worker (2026-09-27, kanban `t_c9e6b0e3`): one
`AddBackend` - dynamic port allocation, writing a config, fork/exec of the
helper and a readiness poll with 10ms granularity - took 79-89ms where the same
path takes ~11ms on an idle machine, and a `time.Sleep(50ms)` inside the helper
stretched to 88ms. Both sides of a race stretch, so a window that looks wide
locally can close there.

Never order a test against a fixed sleep it does not own. Make the helper
record what it did (`VIALITE_HELPER_BACKEND_PIDS`, `VIALITE_HELPER_FORKED_PIDS`)
and gate a deliberate runtime exit on a file the test writes
(`VIALITE_HELPER_EXIT_RELEASE_FILE`), then assert on those markers. A failure
that appears only on a loaded pod is a window that is too narrow (or a leaked
child), not a protocol regression: reproduce it with
`flaky-go-test-reproduction` before calling it a regression.

Poll a condition, do not sample it, when the trigger was an observation the
runtime made: a forked helper publishes its listener as soon as it is exec'd,
which can be before the runner's own post-fork bookkeeping resumes (measured:
sampling `Server.Healthy()` right after a pid marker failed 4/150 loaded runs
with `started=true ready=true healthy=false`).

## Documentation

Keep public operator docs on the Gate website under
`https://gate.minekube.com/vialite/`. This repo should keep implementation,
architecture, and troubleshooting details that are useful to contributors.
