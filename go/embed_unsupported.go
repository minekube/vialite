//go:build vialite_embed && !(linux && (amd64 || arm64))

package vialite

func extractEmbeddedAsset(assetKind) (string, bool, error) {
	return "", false, ErrUnsupportedEmbeddedMode
}
