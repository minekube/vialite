package vialite

const (
	// DefaultMirrorVersion is the pinned release used only when a custom mirror
	// is configured without an explicit Version. Default GitHub downloads use
	// the latest-release API instead.
	DefaultMirrorVersion    = "v0.2.8"
	DefaultDownloadBase     = "https://github.com/minekube/vialite/releases/download"
	DefaultLatestReleaseURL = "https://api.github.com/repos/minekube/vialite/releases/latest"
)
