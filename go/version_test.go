package vialite

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// DefaultMirrorVersion is owned by release-please, not by hand.
//
// .release-please-config.json declares go/version.go as an "extra-files" entry,
// and go/version.go carries the inline annotation release-please's generic
// updater matches - so the release pull request for vX.Y.Z rewrites the constant
// to vX.Y.Z in the same commit that bumps .release-please-manifest.json.
//
// These guards fail closed if that wiring breaks, because a constant that stops
// tracking releases silently pins every file-only mirror to an old runtime (and
// with it an old ViaVersion ceiling). They replace
// TestDefaultMirrorVersionTracksRelease, which pinned a literal and therefore
// needed a hand edit for every release - the manual step this wiring removes.

const (
	releasePleaseConfigPath   = "../.release-please-config.json"
	releasePleaseManifestPath = "../.release-please-manifest.json"

	// releasePleaseVersionAnnotation is the marker release-please's Generic
	// updater looks for (src/updaters/generic.ts, INLINE_UPDATE_REGEX:
	// x-release-please-<major|minor|patch|version-date|version|date>). It
	// rewrites a version on the SAME line as the annotation, and it rewrites the
	// first version-shaped token on that line.
	releasePleaseVersionAnnotation = "x-release-please-version"

	// versionSourceFile is the file that holds the constant, relative to this
	// package directory.
	versionSourceFile = "version.go"
)

var (
	// quotedVersion matches the constant's quoted "vX.Y.Z" literal: group 1 is
	// the tag prefix, group 2 the digits release-please rewrites.
	quotedVersion = regexp.MustCompile(`"(v)([0-9]+\.[0-9]+\.[0-9]+)"`)
	// bareVersion matches the first semver-shaped token on a line - the token
	// release-please replaces (its VERSION_REGEX).
	bareVersion = regexp.MustCompile(`[0-9]+\.[0-9]+\.[0-9]+`)
)

// TestDefaultMirrorVersionIsBumpedByReleasePlease proves the constant still sits
// on a line release-please will rewrite, and that it agrees with the version
// release-please recorded in the manifest. Both halves fail before the wiring
// exists: without the annotation release-please leaves the line untouched, and
// without the automatic bump the constant drifts behind the manifest.
func TestDefaultMirrorVersionIsBumpedByReleasePlease(t *testing.T) {
	source, err := os.ReadFile(versionSourceFile)
	if err != nil {
		t.Fatalf("read %s: %v", versionSourceFile, err)
	}

	var line string
	for _, candidate := range strings.Split(string(source), "\n") {
		trimmed := strings.TrimSpace(candidate)
		if strings.HasPrefix(trimmed, "DefaultMirrorVersion") && strings.Contains(trimmed, "=") {
			line = candidate
			break
		}
	}
	if line == "" {
		t.Fatalf("%s has no DefaultMirrorVersion assignment", versionSourceFile)
	}

	// Half one: the line must stay annotated, or release-please silently stops
	// tracking the constant and it becomes a hand edit again.
	if !strings.Contains(line, releasePleaseVersionAnnotation) {
		t.Fatalf("the DefaultMirrorVersion line must carry the %q annotation that release-please's generic updater matches, or releases stop tracking it: %s",
			releasePleaseVersionAnnotation, strings.TrimSpace(line))
	}

	match := quotedVersion.FindStringSubmatchIndex(line)
	if match == nil {
		t.Fatalf("DefaultMirrorVersion must be a quoted vX.Y.Z literal on the annotated line: %s", strings.TrimSpace(line))
	}
	digits := match[4:6] // the digits release-please rewrites
	literal := "v" + line[digits[0]:digits[1]]

	// The updater replaces the FIRST version-shaped token on the line with the
	// released version ("0.3.5", without a "v"), so the tag prefix has to live
	// outside that token and no other version may appear earlier on the line.
	// Otherwise a release would write the wrong string and every file-only
	// mirror would download a release tag that does not exist.
	if first := bareVersion.FindStringIndex(line); first == nil || first[0] != digits[0] || first[1] != digits[1] {
		t.Fatalf("the first version-shaped token on the DefaultMirrorVersion line must be the constant's own digits (release-please replaces exactly that token): %s",
			strings.TrimSpace(line))
	}

	// Half two: the constant must agree with the version release-please believes
	// is released. A mismatch means the release pull request did not carry the
	// bump (for example because the annotation above was removed).
	if want := "v" + releasePleaseManifestVersion(t); literal != want {
		t.Fatalf("DefaultMirrorVersion = %q but %s says the released version is %q; the release pull request did not bump the constant",
			literal, releasePleaseManifestPath, want)
	}
}

// TestReleasePleaseConfigDeclaresVersionSource proves the extra-files wiring is
// still there, pointing at the file guarded above. Removing it would take the
// bump out of the release pull request without failing anything else.
func TestReleasePleaseConfigDeclaresVersionSource(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	pkgDir := filepath.Base(wd)
	if pkgDir != "go" {
		t.Fatalf("expected to run in the go module directory, got %q", pkgDir)
	}
	want := pkgDir + "/" + versionSourceFile

	data, err := os.ReadFile(releasePleaseConfigPath)
	if os.IsNotExist(err) {
		t.Skipf("%s not found: not a vialite checkout, so the release-please wiring does not apply", releasePleaseConfigPath)
	}
	if err != nil {
		t.Fatalf("read %s: %v", releasePleaseConfigPath, err)
	}

	var config struct {
		Packages map[string]struct {
			ExtraFiles []json.RawMessage `json:"extra-files"`
		} `json:"packages"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse %s: %v", releasePleaseConfigPath, err)
	}

	for _, entry := range config.Packages["."].ExtraFiles {
		if extraFilePath(entry) == want {
			return
		}
	}
	t.Fatalf("%s does not declare %q under packages[\".\"][\"extra-files\"]; release-please would no longer bump DefaultMirrorVersion in the release pull request",
		releasePleaseConfigPath, want)
}

// extraFilePath resolves an extra-files entry, which the schema allows either as
// a plain path or as an object carrying the updater type.
func extraFilePath(entry json.RawMessage) string {
	var plain string
	if err := json.Unmarshal(entry, &plain); err == nil {
		return plain
	}
	var object struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(entry, &object); err == nil {
		return object.Path
	}
	return ""
}

// releasePleaseManifestVersion returns the version release-please recorded for
// the root package. The manifest only exists in a checkout; running the module's
// tests from the module cache (a consumer, not this repository) has nothing to
// guard.
func releasePleaseManifestVersion(t *testing.T) string {
	t.Helper()

	data, err := os.ReadFile(releasePleaseManifestPath)
	if os.IsNotExist(err) {
		t.Skipf("%s not found: not a vialite checkout, so the release-please guard does not apply", releasePleaseManifestPath)
	}
	if err != nil {
		t.Fatalf("read %s: %v", releasePleaseManifestPath, err)
	}

	var manifest map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("parse %s: %v", releasePleaseManifestPath, err)
	}
	version := manifest["."]
	if version == "" {
		t.Fatalf("%s has no %q entry: %s", releasePleaseManifestPath, ".", strings.TrimSpace(string(data)))
	}
	return version
}
