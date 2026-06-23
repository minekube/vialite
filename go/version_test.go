package vialite

import "testing"

func TestDefaultMirrorVersionTracksRelease(t *testing.T) {
	if DefaultMirrorVersion != "v0.2.6" {
		t.Fatalf("DefaultMirrorVersion = %q, want v0.2.6", DefaultMirrorVersion)
	}
}
