//go:build vialite_embed && linux && arm64

package vialite

import _ "embed"

//go:embed assets/vialite-linux-arm64
var embeddedBinary []byte

//go:embed assets/libvialite-linux-arm64.so
var embeddedLibrary []byte

func extractEmbeddedAsset(kind assetKind) (string, bool, error) {
	switch kind {
	case assetKindBinary:
		return extractBytes("vialite-linux-arm64", embeddedBinary, 0o755)
	case assetKindLibrary:
		return extractBytes("libvialite-linux-arm64.so", embeddedLibrary, 0o644)
	default:
		return "", false, nil
	}
}
