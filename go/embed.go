package vialite

func extractEmbeddedBinary() (string, bool, error) {
	return extractEmbeddedAsset(assetKindBinary)
}

func extractEmbeddedLibrary() (string, bool, error) {
	return extractEmbeddedAsset(assetKindLibrary)
}
