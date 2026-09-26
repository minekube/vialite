package vialite

const (
	// DefaultMirrorVersion is the release vialite falls back to when a custom
	// Mirror is configured, the requested Version is unset, and the mirror
	// cannot report its own latest release. Default GitHub downloads resolve the
	// latest release through the GitHub API instead, and a mirror that answers
	// `<mirror>/latest` is followed too, so this constant only ever applies to
	// file-only mirrors — where vialite logs a warning naming it.
	//
	// Never hand-edit the version. This file is declared under "extra-files" in
	// .release-please-config.json and the trailing annotation below is the marker
	// release-please's generic updater matches, so the release pull request for
	// vX.Y.Z rewrites the constant together with .release-please-manifest.json.
	// It has to stay on a released version: file-only mirrors download it from
	// DefaultDownloadBase, and .github/workflows/bump-upstream-pin.yml fails when
	// it trails `releases/latest` by more than one release. See
	// docs/release-runbook.md and the guards in version_test.go.
	DefaultMirrorVersion    = "v0.3.6" // x-release-please-version
	DefaultDownloadBase     = "https://github.com/minekube/vialite/releases/download"
	DefaultLatestReleaseURL = "https://api.github.com/repos/minekube/vialite/releases/latest"
)
