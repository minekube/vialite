//go:build !vialite_embed

package vialite

func extractEmbeddedAsset(assetKind) (string, bool, error) {
	return "", false, nil
}
