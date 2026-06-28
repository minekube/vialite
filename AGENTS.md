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
- Renovate tracks ViaProxy upstream and opens dependency PRs, but agents should
  still inspect upstream diffs and CI results before assuming a bump is safe.
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

## Documentation

Keep public operator docs on the Gate website under
`https://gate.minekube.com/vialite/`. This repo should keep implementation,
architecture, and troubleshooting details that are useful to contributors.
