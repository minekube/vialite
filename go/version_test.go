package vialite

import "testing"

func TestDefaultVersionTracksLatestRelease(t *testing.T) {
	if DefaultVersion != "v0.2.2" {
		t.Fatalf("DefaultVersion = %q, want v0.2.2", DefaultVersion)
	}
}
