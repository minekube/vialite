//go:build !unix && !windows

package vialite

func loadNativeSymbols(string) (*nativeSymbols, error) {
	return nil, ErrUnsupportedEmbeddedMode
}
