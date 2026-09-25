package vialite

const (
	// DefaultMirrorVersion is the release vialite falls back to when a custom
	// Mirror is configured, the requested Version is unset, and the mirror
	// cannot report its own latest release. Default GitHub downloads resolve the
	// latest release through the GitHub API instead, and a mirror that answers
	// `<mirror>/latest` is followed too, so this constant only ever applies to
	// file-only mirrors — where vialite logs a warning naming it.
	//
	// Keep it on the current release: .github/workflows/bump-upstream-pin.yml
	// fails when it trails `releases/latest` by more than one release.
	DefaultMirrorVersion    = "v0.3.1"
	DefaultDownloadBase     = "https://github.com/minekube/vialite/releases/download"
	DefaultLatestReleaseURL = "https://api.github.com/repos/minekube/vialite/releases/latest"
)
