package vialite

import "testing"

func TestDefaultVersionTracksLatestRelease(t *testing.T) {
	if DefaultVersion != "v0.2.4" {
		t.Fatalf("DefaultVersion = %q, want v0.2.4", DefaultVersion)
	}
}
