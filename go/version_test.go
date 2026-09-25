package vialite

import "testing"

func TestDefaultMirrorVersionTracksRelease(t *testing.T) {
	if DefaultMirrorVersion != "v0.3.3" {
		t.Fatalf("DefaultMirrorVersion = %q, want v0.3.3", DefaultMirrorVersion)
	}
}
