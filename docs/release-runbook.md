# Release runbook: upstream ViaProxy / ViaVersion -> vialite -> Gate

ViaVersion ships *inside* the vialite release artifact: the API in
[`build/via.version`](../build/via.version) pins a ViaProxy ref and ViaProxy's
`build.gradle` bundles `viaversion-common`. A new Minecraft client version is
therefore only reachable for operators once upstream releases, the pin moves,
the native build goes green, and a new vialite release is published. This page
is that chain, end to end, with the parts that are still manual called out.

## 1. What is already automatic

`.github/workflows/bump-upstream-pin.yml` runs daily (03:17 UTC) and on
`workflow_dispatch`. It:

1. resolves the latest stable upstream ViaProxy **release** (not the default
   branch: `main` bundles ViaVersion snapshots),
2. compares its commit to `build/via.version` and reports the bundled
   ViaVersion of the pinned ref against the latest upstream ViaVersion release,
3. pushes `automation/bump-viaproxy` and opens or refreshes a pull request with
   the upstream diff, the ViaVersion comparison, and a "before merging"
   checklist,
4. dispatches `ci.yml` and `native-image.yml` on that branch (`craftless.yml`
   when `craftless: true`) and posts the results on the pull request.

It **never merges**: a pin bump is a code change, not a lockfile refresh.

Useful dispatches:

```sh
# report only: resolve the candidate and print what the pin would become
gh workflow run bump-upstream-pin.yml --repo minekube/vialite \
  -f dry_run=true

# pin an explicit upstream ref (e.g. the release carrying a brand-new protocol)
gh workflow run bump-upstream-pin.yml --repo minekube/vialite \
  -f ref=v3.4.13 -f craftless=true

# current upstream state without running anything
gh api repos/ViaVersion/ViaProxy/releases/latest --jq .tag_name
gh api repos/ViaVersion/ViaVersion/releases/latest --jq .tag_name
```

## 2. The manual remainder (what this runbook exists for)

### 2.1 Re-derive the overlay

`build/overlay/` ships **adapted copies** of upstream files, not patches. A new
pin silently reverts every upstream change in those files, which usually breaks
the native build (renamed classes, new accessors). Fetch the upstream file at
the new ref, re-apply only the documented deltas, and keep the delta list in
[`build/overlay/README.md`](../build/overlay/README.md) in sync. Cheap
pre-flight without Docker:

```sh
bash build/apply-overlay.sh
cd build/.work/ViaProxy && ./gradlew compileJava :vialite-native:compileJava
```

### 2.2 Merge the bump, then release

- Merge the bump PR once `ci.yml` and `native-image.yml` are green on its head.
  A `fix(deps):` merge drives release-please.
- Review and merge the release PR (`chore(main): release X.Y.Z`) that
  release-please opens. `release.yml` then publishes the checksummed
  artifacts on the tag.
- **Wait for the artifacts before telling anyone the release is out.**
  Operators with an unset `via.version` resolve `releases/latest` at every
  Gate start, so a tag with no assets is a broken release for them:

```sh
gh release view vX.Y.Z --repo minekube/vialite --json tagName,isDraft,assets \
  --jq '{tag:.tagName,draft:.isDraft,assets:[.assets[].name]}'
```

- Bump `DefaultMirrorVersion` in [`go/version.go`](../go/version.go) to the new
  tag if it trails (the daily workflow fails when it trails by more than one
  release, so a green daily run is the check).

### 2.3 Downstream Gate Go module (currently by hand)

The release is supposed to dispatch `minekube/gate`
`bump-managed-dependency.yml`. It does not today: vialite's
`RELEASE_CASCADE_APP_PRIVATE_KEY` went stale in the 2026-08-28 credential
rotation (`A JSON web token could not be decoded` in the `dispatch-gate-bump`
job), and rotating a repository secret is a credential change that needs the
owner. Until then, dispatch the Gate bump manually:

```sh
gh workflow run bump-managed-dependency.yml --repo minekube/gate \
  -f module=go.minekube.com/vialite -f version=vX.Y.Z -f repository=minekube/vialite
```

This only moves the **Go library** Gate links (lifecycle code, resolution
behaviour, resolution logging). It does not change which runtime artifact an
operator downloads: Gate resolves the artifact per start from
`releases/latest`, so customers get a new runtime on a Gate restart without any
Gate upgrade.

## 3. Verifying a release the way a customer experiences it

1. Tag public with assets (`checksums.txt` plus the per-platform artifacts).
2. Module resolvable: `go list -m go.minekube.com/vialite@vX.Y.Z`.
3. Runtime reports the new ceiling: start Gate with `via.enabled: true` (no
   `version` pin) and confirm the resolution line
   (`vialite: resolved runtime ...`) followed by the runtime's own
   `Highest supported version by the proxy: <version> (<protocol>)`.
4. A protocol-newer client can join a protocol-older backend; the Craftless
   matrix row `subprocess-newer-client-older-backend` in
   [`test/craftless/matrix.json`](../test/craftless/matrix.json) is the
   in-repo proof for that pair.

## 4. When the workflow is red or silent

- `changed=false` in the `Resolve upstream candidate` step means the pin already
  tracks upstream. That is the normal steady state, not a failure.
- A red `native-image.yml` on the bump branch means the overlay needs work
  (§2.1).
- A red daily run whose summary carries an "upstream ViaVersion newer" warning
  means upstream released a ViaVersion that no ViaProxy release bundles yet:
  wait for upstream's next ViaProxy release, or pin `main` deliberately with
  `-f source=branch` (ships a ViaVersion snapshot - only for a brand-new
  protocol that cannot wait).
