package vialite

import (
	"fmt"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const (
	nativeBuildScriptPath   = "../build/overlay/vialite-native/build.gradle.kts"
	nativeImageWorkflowPath = "../.github/workflows/native-image.yml"

	// nativeArm64PageSize is the page size the arm64 native image is built for.
	// It has to cover every kernel the artifact can meet in the field, and the
	// largest common one is 64 KiB; 64 KiB alignment also runs on 4 KiB and
	// 16 KiB kernels, because an image may run wherever the run-time page size
	// is <= the build-time page size.
	nativeArm64PageSize = "65536"
)

// A GraalVM native image records the page size of the machine that *built* it,
// and it may only run where the run-time page size is less than or equal to
// that value — SubstrateOptions.getPageSize(), "-H:PageSize=<n> … The default =
// 0 (== same as host machine page size)", with the invariant spelled out in
// graalvm-community-jdk21u: "images can be executed as long as run-time page
// size <= build-time page size".
//
// vialite builds the arm64 artifacts on ubuntu-24.04-arm, a 4 KiB-page machine,
// and passed no -H:PageSize, so the released v0.3.11 vialite-linux-arm64 and
// libvialite-linux-arm64.so carry 4096-byte section alignment for
// .text/.rodata/.svm_heap/.data. On a 16 KiB-page kernel — the Raspberry Pi 5
// firmware's default kernel_2712.img, and every Asahi Linux host — the image
// cannot start at all, before main:
//
//	INFO vialite: resolved runtime kind=binary source=cache version=v0.3.11
//	Fatal error: Failed to create the main Isolate. (code 24)
//	error running Gate: error starting vialite: exit status 24
//
// No config.yml value can change that, and `via.mode: embedded` links the same
// 4 KiB library, so the fix belongs in the build (kanban t_11ddda2b, customer
// Bark / man900 on a Pi 5 with DietPi). 64 KiB page size is what apple/pkl took
// in 267de3c for exactly these two platforms, and coursier#2636 is the same
// failure on Asahi.
//
// Two inputs have to keep holding, and neither is observable from a Go unit test
// on its own, so this guard pins them:
//
//  1. build/overlay/vialite-native/build.gradle.kts passes -H:PageSize=65536
//     for Linux aarch64. It is one binary target, so the flag covers both the
//     executable (-Pvialite.native.shared=false) and the shared library.
//  2. .github/workflows/native-image.yml reads the section alignment back off
//     the built arm64 artifacts, so a runner or GraalVM pin change that
//     silently reverts to a 4 KiB image fails CI instead of reaching operators.
//
// The run-time confirmation has to come from a machine with a 16 KiB page size
// (the reporter's Pi 5); this cluster has no such host, and CI's arm64 runner is
// itself 4 KiB, which is exactly why the alignment assertion — not a run — is
// what CI can prove.
func TestNativeArm64ImageTargetsSixtyFourKiBPageSize(t *testing.T) {
	script := string(releaseCheckReadFile(t, nativeBuildScriptPath))

	if !strings.Contains(script, "aarch64") || !strings.Contains(script, "os.arch") {
		t.Error("build.gradle.kts no longer detects a Linux aarch64 build, so the page-size flag would have to apply to every target")
	}
	if !strings.Contains(script, fmt.Sprintf("getOrElse(if (isLinuxAarch64) %q else \"\")", nativeArm64PageSize)) {
		t.Errorf("build.gradle.kts no longer defaults -H:PageSize to %s for Linux aarch64; without it the image carries the CI runner's 4 KiB pages and cannot start on a 16 KiB kernel", nativeArm64PageSize)
	}
	if !strings.Contains(script, `"-H:PageSize=$bytes"`) {
		t.Error("build.gradle.kts no longer passes -H:PageSize to native-image")
	}
	if !strings.Contains(script, "(bytes and (bytes - 1)) != 0") {
		t.Error("build.gradle.kts no longer rejects a page size that is not a power of two >= 4096; a bad value is a native-image or linker failure far into a 20 minute build")
	}

	guarded := strings.Index(script, "if (nativePageSize.isNotEmpty())")
	applied := strings.Index(script, `args.add("-H:PageSize=$bytes")`)
	if guarded < 0 {
		t.Fatal("build.gradle.kts no longer gates the page-size flag, so it would be passed to amd64 and Windows builds as well")
	}
	if applied < 0 || applied < guarded {
		t.Error("the -H:PageSize argument must be added inside the nativePageSize guard, otherwise amd64/Windows images inherit a 64 KiB page assumption they do not need")
	}
}

// TestNativeImageWorkflowAssertsArm64PageAlignment pins the CI half of the same
// fix: the workflow has to read the alignment back off the artifacts it just
// built, on every event (a pull_request run is where a regression should be
// caught, not only a push to main), and it has to fail closed with an ::error
// annotation instead of shipping a 4 KiB image.
func TestNativeImageWorkflowAssertsArm64PageAlignment(t *testing.T) {
	raw := releaseCheckReadFile(t, nativeImageWorkflowPath)
	doc := releaseCheckParseWorkflow(t, nativeImageWorkflowPath, raw)

	root := doc.Content[0]
	build := releaseCheckMappingValue(releaseCheckMappingValue(root, "jobs"), "build")
	if build == nil {
		t.Fatalf("%s has no `build` job any more; the arm64 artifacts are built there", nativeImageWorkflowPath)
	}
	steps := releaseCheckMappingValue(build, "steps")

	var (
		found     bool
		stepIf    string
		stepRun   string
		stepIndex = -1
	)
	if steps != nil && steps.Kind == yaml.SequenceNode {
		for i, step := range steps.Content {
			run := releaseCheckMappingValue(step, "run")
			if run == nil || !strings.Contains(run.Value, "readelf") {
				continue
			}
			found = true
			stepIndex = i
			stepRun = run.Value
			if cond := releaseCheckMappingValue(step, "if"); cond != nil {
				stepIf = cond.Value
			}
			break
		}
	}
	if !found {
		t.Fatalf("%s no longer asserts the arm64 section alignment with readelf; a runner or GraalVM pin change would then reintroduce 4 KiB-page arm64 images silently (v0.3.11 shipped that way and every 16 KiB kernel - Raspberry Pi 5 default kernel, Asahi - cannot start it)", nativeImageWorkflowPath)
	}

	if !strings.Contains(stepIf, "arm64") {
		t.Errorf("the alignment assertion runs with `if: %s`; it must be limited to the arm64 matrix leg, because amd64 and Windows images keep the host page size on purpose", stepIf)
	}
	if strings.Contains(stepIf, "github.event_name") {
		t.Error("the alignment assertion is gated on the event; it must also run for pull_request builds, which is where a regression is caught before release")
	}
	if !strings.Contains(stepRun, "docker cp") {
		t.Error("the alignment assertion no longer reads the artifacts out of the built image, so it could be inspecting a file that was not just built")
	}
	for _, artifact := range []string{"vialite-linux-arm64", "libvialite-linux-arm64.so"} {
		if !strings.Contains(stepRun, artifact) {
			t.Errorf("the alignment assertion no longer covers %s", artifact)
		}
	}
	if !strings.Contains(stepRun, ".svm_heap") {
		t.Error("the alignment assertion no longer checks .svm_heap, the section GraalVM lays out with the build-time page size")
	}
	if !strings.Contains(stepRun, "::error") {
		t.Error("the alignment assertion no longer fails with an ::error annotation naming the artifact and section")
	}
	if !strings.Contains(stepRun, "65536") {
		t.Errorf("the alignment assertion no longer requires at least %s bytes of alignment", nativeArm64PageSize)
	}
	if stepIndex >= 0 && !strings.Contains(stepRun, "set -euo pipefail") {
		t.Error("the alignment assertion step does not run under `set -euo pipefail`, so a failing docker or readelf invocation could pass as success")
	}
}
