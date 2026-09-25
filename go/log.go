package vialite

import "log/slog"

// resolutionLogger returns the logger used for runtime resolution messages.
//
// Options.validate defaults Logger to slog.Default(), but download/locate run
// with raw Options too (and in tests), so resolve it defensively here.
func (o Options) resolutionLogger() *slog.Logger {
	if o.Logger != nil {
		return o.Logger
	}
	return slog.Default()
}

// runtimeKindName names the artifact kind in operator-facing messages.
func runtimeKindName(kind assetKind) string {
	if kind == assetKindLibrary {
		return "library"
	}
	return "binary"
}

// logRuntimeResolution records which native runtime is actually in use, where
// it came from, and — for downloaded artifacts — the release version it was
// taken from.
//
// This matters operationally: the runtime release decides the ViaVersion
// protocol ceiling the proxy can translate, and before this line the only place
// that surfaced it was the runtime's own subprocess output
// ("Highest supported version by the proxy: ..."). One line at startup makes the
// answer greppable for support and for operators.
func logRuntimeResolution(opts Options, kind string, attrs ...any) {
	base := []any{"kind", kind}
	base = append(base, attrs...)
	opts.resolutionLogger().Info("vialite: resolved runtime", base...)
}
