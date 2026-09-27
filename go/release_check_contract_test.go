// SPDX-License-Identifier: MIT
package vialite

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// Every release PR shows *.two* red checks — `CI` and `Craftless compatibility`
// — and neither can ever go green. That is not a broken job and it is not the
// `on.pull_request.paths` filter: it is GitHub's documented approval gate for
// pull requests that a workflow created with `GITHUB_TOKEN`.
//
//	"When a pull request is created or updated by a workflow using
//	 `GITHUB_TOKEN`, `pull_request` events with the `opened`, `synchronize`, or
//	 `reopened` activity types create workflow runs that require approval. A
//	 user with write access to the repository can approve these runs from the
//	 pull request page. With the exception of `workflow_dispatch` and
//	 `repository_dispatch`, other `GITHUB_TOKEN`-triggered events do not create
//	 workflow runs at all."
//	— GitHub docs, "Events that trigger workflows" -> `pull_request`
//
// Observed on this repository (2026-09-27, read-only): release 0.3.7 (PR #41,
// head 0c57342c) produced runs 36312571573 (`CI`) and 36312571624 (`Craftless
// compatibility`); release 0.3.6 (PR #39, head 26408d9a) produced the same pair,
// 36204128268 and 36204128261. All four carry `event=pull_request`,
// `actor=github-actions[bot]`, `jobs.total_count=0`, `conclusion=failure`, and
// they are the only `github-actions[bot]` `pull_request` runs in the whole
// action history. That is the two-per-release shape this repository has, and it
// is why `go/release_check_contract_test.go` pins *both* workflows.
//
// The release branch therefore behaves like this, and both halves are pinned
// below because the acceptance notes in the two workflows depend on them:
//
//   - ci.yml and craftless.yml *are* triggered on a release PR: release-please
//     rewrites go/version.go (declared under "extra-files" in
//     .release-please-config.json; PR #37 added the `x-release-please-version`
//     annotation its generic updater matches) together with
//     .release-please-manifest.json and CHANGELOG.md, so `go/**` matches both
//     filters.
//   - native-image.yml is *not* triggered on a release PR (its filter is
//     `build/**` and its own file), which is the control: a workflow whose
//     `paths` filter does not match produces no run at all, not a failed one.
//
// The history shows the same boundary: release PRs 0.3.1-0.3.5 (PR #33, #35,
// #38) touched only .release-please-manifest.json and CHANGELOG.md, matched
// neither filter, and produced **no** runs; the phantom pair first appears with
// 0.3.6, the first release PR that carries go/version.go.
//
// Consequence, and the reason this test exists: no check context reported by
// either workflow — the workflow entries `CI` / `Craftless compatibility`, nor
// the job names (`go-test`, `docs-lint`, `validate Craftless matrix`,
// `build PR subprocess linux amd64`, the per-row
// `subprocess client <v> -> server <v>`) — may be added to the required status
// checks of `main`. No release PR can ever satisfy them: the run is created only
// to sit in `action_required` until the PR is merged, at which point GitHub
// records it as `failure`. release-please.yml's auto-merge step would then fail
// and the release chain would stall on every release. `main` requires nothing
// today (GET /repos/minekube/vialite/branches/main/protection -> 404 "Branch not
// protected", read with an installation token holding `administration: read`;
// the same call returns `contexts=['lint-test']` for minekube/geyserlite), and
// unlike geyserlite this repository mirrors **no** context at all, so there is
// no mirror that could satisfy a required gated check.
//
// The only way to make the checks real is to have release-please open its PR
// with a GitHub App/PAT token instead of `GITHUB_TOKEN` (GitHub docs,
// "Triggering a workflow from a workflow": an App installation token or PAT
// "also lets `pull_request` workflows run automatically (without the approval
// prompt described above) when the pull request is created or updated by
// automation"). That is a credential change, not a workflow edit, so it is
// deliberately not done here.
const releaseCheckContractPrefix = "release-check contract:"

const (
	releaseCheckReleasePleaseWorkflowPath = ".github/workflows/release-please.yml"
	releaseCheckControlWorkflowPath       = ".github/workflows/native-image.yml"

	// releaseCheckNoteMarker and releaseCheckRequiredCheckRule pin the
	// acceptance notes themselves. Both gated workflows must carry them
	// verbatim: if the note goes away the guard below protects a decision that
	// is no longer written down anywhere.
	releaseCheckNoteMarker        = "Acceptance note"
	releaseCheckRequiredCheckRule = "must never become required status checks on"
)

// releaseCheckReleasePRFiles is the file set release-please writes into its own
// release PR. Observed on PR #41 (0.3.7) and PR #39 (0.3.6) and unchanged for
// the earlier releases, where it was only the last two entries — that
// difference is what makes the filters match (and the phantom runs appear) from
// 0.3.6 onwards.
var releaseCheckReleasePRFiles = []string{
	".release-please-manifest.json",
	"CHANGELOG.md",
	"go/version.go",
}

// releaseCheckWantMirroredContexts is the set of branch-protection contexts
// release-please.yml mirrors for a release PR. This repository has no mirror
// step at all (geyserlite, where `main` requires exactly `['lint-test']`, has
// one): `main` requires nothing, so the correct set is empty. A non-empty set
// means someone started satisfying contexts for branch protection and the
// required-check rule in the acceptance notes has to be re-decided first.
var releaseCheckWantMirroredContexts []string

// releaseCheckWorkflow is one workflow file and the facts about it that the
// disposition rests on.
type releaseCheckWorkflow struct {
	path string
	// name is the workflow `name:`, i.e. the display name of the entry the
	// release PR shows as red.
	name string
	// paths is `on.pull_request.paths`; nil means the workflow no longer
	// filters that trigger by path.
	paths []string
}

// releaseCheckGatedWorkflows are the two workflows GitHub creates an
// approval-gated run for on every release PR. Order matters for the mutation
// table below.
func releaseCheckGatedWorkflows() []releaseCheckWorkflow {
	return []releaseCheckWorkflow{
		{path: ".github/workflows/ci.yml", name: "CI"},
		{path: ".github/workflows/craftless.yml", name: "Craftless compatibility"},
	}
}

// releaseCheckContract is every workflow fact the acceptance decisions in
// ci.yml and craftless.yml rest on. It is a plain value so the mutation table
// below can rebuild it.
type releaseCheckContract struct {
	gated                   []releaseCheckWorkflow
	control                 releaseCheckWorkflow
	noteContents            map[string]string
	releasePleaseTokenInput string
	autoMergeStepRun        string
	mirroredContexts        []string
}

// releaseCheckLiveContract reads the four workflows and assembles the contract.
func releaseCheckLiveContract(t *testing.T, dir string) releaseCheckContract {
	t.Helper()
	c := releaseCheckContract{noteContents: map[string]string{}}
	for _, want := range releaseCheckGatedWorkflows() {
		raw := releaseCheckReadFile(t, filepath.Join(dir, want.path))
		doc := releaseCheckParseWorkflow(t, want.path, raw)
		want.paths = releaseCheckTriggerPaths(t, want.path, doc, "pull_request")
		c.gated = append(c.gated, want)
		c.noteContents[want.path] = string(raw)
	}

	controlRaw := releaseCheckReadFile(t, filepath.Join(dir, releaseCheckControlWorkflowPath))
	controlDoc := releaseCheckParseWorkflow(t, releaseCheckControlWorkflowPath, controlRaw)
	c.control = releaseCheckWorkflow{
		path:  releaseCheckControlWorkflowPath,
		name:  releaseCheckScalar(controlDoc, "name"),
		paths: releaseCheckTriggerPaths(t, releaseCheckControlWorkflowPath, controlDoc, "pull_request"),
	}

	c.releasePleaseTokenInput = releaseCheckReleasePleaseToken(t, dir)
	c.autoMergeStepRun = releaseCheckAutoMergeStepRun(t, dir)
	c.mirroredContexts = releaseCheckMirroredContexts(c.autoMergeStepRun)
	return c
}

// validateReleaseCheckContract fails when any fact the documented disposition in
// the two acceptance notes rests on has changed. The messages name the
// disposition so a future reader re-decides it instead of removing the guard.
func validateReleaseCheckContract(c releaseCheckContract, releaseFiles []string) error {
	want := releaseCheckGatedWorkflows()
	if len(c.gated) != len(want) {
		return fmt.Errorf("%d workflows are pinned as approval-gated, want %d (%s); a release PR costs one red "+
			"check per gated workflow, so both acceptance notes state the count and have to be re-decided",
			len(c.gated), len(want), releaseCheckWorkflowPaths(want))
	}
	for i, w := range want {
		got := c.gated[i]
		if got.path != w.path || got.name != w.name {
			return fmt.Errorf("the approval-gated workflow %d is %s (%q), want %s (%q); the acceptance notes name the "+
				"red check of each workflow explicitly, so renaming one re-decides them",
				i+1, got.path, got.name, w.path, w.name)
		}
	}

	for _, w := range c.gated {
		if len(w.paths) == 0 {
			return fmt.Errorf("%s no longer filters `pull_request` by `paths`; the acceptance note in that file says "+
				"the release PR matches the filter on purpose, and the pre-0.3.6 history (release PRs that matched no "+
				"filter produced no run at all) is part of the mechanism's evidence", w.path)
		}
		if matched := releaseCheckMatchedFiles(w.paths, releaseFiles); len(matched) == 0 {
			return fmt.Errorf("%s no longer triggers on a release PR: none of %v matches %v. The approval-gated run "+
				"cannot exist then, and the red check on every release PR disappears - which looks like a fix but only "+
				"trades a red check for a missing one. Re-decide the acceptance note in that file before updating this test",
				w.path, releaseFiles, w.paths)
		}
	}

	if len(c.control.paths) == 0 {
		return fmt.Errorf("%s no longer filters `pull_request` by `paths`; it is the control that shows a "+
			"non-matching filter creates no run at all", releaseCheckControlWorkflowPath)
	}
	if matched := releaseCheckMatchedFiles(c.control.paths, releaseFiles); len(matched) > 0 {
		return fmt.Errorf("%s now triggers on release PRs as well (%v matches %v); the control that shows a "+
			"non-matching `paths` filter creates no run at all - and therefore that the red checks are the approval "+
			"gate and not the filters - no longer holds",
			releaseCheckControlWorkflowPath, matched, c.control.paths)
	}

	if c.releasePleaseTokenInput != "" {
		return fmt.Errorf("the release-please action now receives a token (%q), so its pull request is no longer "+
			"created with `GITHUB_TOKEN`; the approval gate the acceptance notes describe does not apply anymore. "+
			"Re-check whether the gated runs are now green and whether a check context from %s may become required",
			c.releasePleaseTokenInput, releaseCheckWorkflowPaths(want))
	}

	if !strings.Contains(c.autoMergeStepRun, "gh pr merge") {
		return fmt.Errorf("%s no longer merges the release PR with `gh pr merge`; the stall the acceptance notes warn "+
			"about assumes release-please's own auto-merge step is what would fail on an unsatisfiable required check",
			releaseCheckReleasePleaseWorkflowPath)
	}
	if strings.Contains(c.autoMergeStepRun, "--admin") {
		return fmt.Errorf("%s merges the release PR with `--admin`, which bypasses required status checks; that would "+
			"silently turn the latent required-check deadlock the acceptance notes describe into a working-looking "+
			"release chain, so the disposition has to be re-decided", releaseCheckReleasePleaseWorkflowPath)
	}

	for _, path := range releaseCheckWorkflowPaths(want) {
		note := releaseCheckNormalizeNote(c.noteContents[path])
		if !strings.Contains(note, releaseCheckNoteMarker) {
			return fmt.Errorf("%s carries no `%s` block; the accepted red check is then only documented in "+
				"go/release_check_contract_test.go, which is exactly what this guard is supposed to prevent",
				path, releaseCheckNoteMarker)
		}
		if !strings.Contains(note, releaseCheckRequiredCheckRule) {
			return fmt.Errorf("the acceptance note in %s no longer states that the contexts this workflow reports "+
				"%q; that is the disposition this test pins, so removing the sentence must fail here instead of "+
				"silently un-pinning the rule", path, releaseCheckRequiredCheckRule)
		}
	}

	mirrored := append([]string(nil), c.mirroredContexts...)
	sort.Strings(mirrored)
	expected := append([]string(nil), releaseCheckWantMirroredContexts...)
	sort.Strings(expected)
	if strings.Join(mirrored, ",") != strings.Join(expected, ",") {
		return fmt.Errorf("%s mirrors %v for branch protection, want exactly %v. This repository has no mirror step: "+
			"`main` requires no context, so mirroring one means a gated context was made satisfiable and the "+
			"required-check rule in the acceptance notes has to be re-decided first",
			releaseCheckReleasePleaseWorkflowPath, c.mirroredContexts, releaseCheckWantMirroredContexts)
	}
	return nil
}

func releaseCheckWorkflowPaths(workflows []releaseCheckWorkflow) []string {
	paths := make([]string, 0, len(workflows))
	for _, w := range workflows {
		paths = append(paths, w.path)
	}
	return paths
}

// releaseCheckNormalizeNote strips the YAML comment markers and joins the lines,
// so the pinned sentences below are found regardless of where the note is
// wrapped. Reflowing a comment must not fail this guard; deleting its content
// must.
func releaseCheckNormalizeNote(contents string) string {
	var builder strings.Builder
	for _, line := range strings.Split(contents, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") {
			line = strings.TrimPrefix(line, "#")
		}
		builder.WriteString(strings.Join(strings.Fields(line), " "))
		builder.WriteString(" ")
	}
	return builder.String()
}

// TestReleaseBranchCheckContractMatchesLiveWorkflows is the guard: the live
// workflows must still satisfy every fact the accepted-noise disposition in the
// two acceptance notes depends on.
func TestReleaseBranchCheckContractMatchesLiveWorkflows(t *testing.T) {
	contract := releaseCheckLiveContract(t, "..")
	if err := validateReleaseCheckContract(contract, releaseCheckReleasePRFiles); err != nil {
		t.Fatalf("%s %v", releaseCheckContractPrefix, err)
	}
	if contract.releasePleaseTokenInput != "" {
		t.Fatalf("%s the release-please step must not pass a token, got %q",
			releaseCheckContractPrefix, contract.releasePleaseTokenInput)
	}
	if len(contract.gated) != 2 {
		t.Fatalf("%s want the two approval-gated workflows, got %d", releaseCheckContractPrefix, len(contract.gated))
	}
}

// TestReleaseBranchCheckContractRejectsMutations proves the guard has teeth:
// each mutation below invalidates a fact the disposition rests on and must be
// rejected with a message that names it.
func TestReleaseBranchCheckContractRejectsMutations(t *testing.T) {
	live := releaseCheckLiveContract(t, "..")
	ci := releaseCheckGatedWorkflows()[0]
	craftless := releaseCheckGatedWorkflows()[1]
	mutations := map[string]func(c releaseCheckContract) releaseCheckContract{
		"ci-renamed": func(c releaseCheckContract) releaseCheckContract {
			c.gated[0].name = "ci"
			return c
		},
		"craftless-renamed": func(c releaseCheckContract) releaseCheckContract {
			c.gated[1].name = "craftless"
			return c
		},
		"craftless-no-longer-gated": func(c releaseCheckContract) releaseCheckContract {
			c.gated = c.gated[:1]
			return c
		},
		"ci-paths-filter-dropped": func(c releaseCheckContract) releaseCheckContract {
			c.gated[0].paths = nil
			return c
		},
		"ci-release-files-no-longer-match": func(c releaseCheckContract) releaseCheckContract {
			c.gated[0].paths = []string{"examples/**", "docs/**"}
			return c
		},
		"craftless-release-files-no-longer-match": func(c releaseCheckContract) releaseCheckContract {
			c.gated[1].paths = []string{"test/craftless/**", ".github/workflows/craftless.yml"}
			return c
		},
		"control-now-matches": func(c releaseCheckContract) releaseCheckContract {
			c.control.paths = append(append([]string(nil), c.control.paths...), "go/**")
			return c
		},
		"control-filter-dropped": func(c releaseCheckContract) releaseCheckContract {
			c.control.paths = nil
			return c
		},
		"release-please-token-added": func(c releaseCheckContract) releaseCheckContract {
			c.releasePleaseTokenInput = "${{ secrets.RELEASE_PLEASE_TOKEN }}"
			return c
		},
		"admin-merge-introduced": func(c releaseCheckContract) releaseCheckContract {
			c.autoMergeStepRun = strings.Replace(c.autoMergeStepRun, "gh pr merge", "gh pr merge --admin", 1)
			return c
		},
		"auto-merge-step-removed": func(c releaseCheckContract) releaseCheckContract {
			c.autoMergeStepRun = "echo no merge here"
			return c
		},
		"craftless-note-deleted": func(c releaseCheckContract) releaseCheckContract {
			c.noteContents[craftless.path] = strings.ReplaceAll(
				c.noteContents[craftless.path], releaseCheckRequiredCheckRule, "may be required")
			return c
		},
		"ci-note-marker-removed": func(c releaseCheckContract) releaseCheckContract {
			c.noteContents[ci.path] = strings.ReplaceAll(c.noteContents[ci.path], releaseCheckNoteMarker, "Note")
			return c
		},
		"gated-context-mirrored": func(c releaseCheckContract) releaseCheckContract {
			c.mirroredContexts = append(append([]string(nil), c.mirroredContexts...), craftless.name)
			return c
		},
	}
	for name, mutate := range mutations {
		t.Run(name, func(t *testing.T) {
			err := validateReleaseCheckContract(mutate(live), releaseCheckReleasePRFiles)
			if err == nil {
				t.Fatalf("%s mutation %q was accepted", releaseCheckContractPrefix, name)
			}
			if !strings.HasPrefix(err.Error(), releaseCheckContractPrefix) &&
				!strings.Contains(err.Error(), ".github/workflows/") {
				t.Fatalf("%s mutation %q failed without naming the disposition: %v", releaseCheckContractPrefix, name, err)
			}
		})
	}
}

// TestReleaseCheckPathMatcher pins the matcher used above: GitHub's `paths`
// patterns with `**` spanning separators and `*` staying inside one segment.
func TestReleaseCheckPathMatcher(t *testing.T) {
	cases := []struct {
		pattern string
		path    string
		want    bool
	}{
		{"go/**", "go/version.go", true},
		{"go/**", "gofmt/x.go", false},
		{"go/**", "go/a/b/c.go", true},
		{"build/**", "build/via.version", true},
		{"build/**", "go/version.go", false},
		{"mise.toml", "mise.toml", true},
		{"Taskfile.yml", "Taskfile.yml", true},
		{"CHANGELOG.md", "CHANGELOG.md", true},
		{"docs/**", "docs/release-runbook.md", true},
		{".release-please-manifest.json", ".release-please-manifest.json", true},
		{".github/workflows/craftless.yml", ".github/workflows/craftless.yml", true},
		{".github/workflows/craftless.yml", ".github/workflows/ci.yml", false},
		{"test/craftless/**", "test/craftless/matrix.json", true},
	}
	for _, tc := range cases {
		if got := releaseCheckMatchPath(tc.pattern, tc.path); got != tc.want {
			t.Errorf("%s path %q against pattern %q = %v, want %v", releaseCheckContractPrefix, tc.path, tc.pattern, got, tc.want)
		}
	}
}

// releaseCheckMatchedFiles returns the release PR files matching any pattern.
func releaseCheckMatchedFiles(patterns, files []string) []string {
	var matched []string
	for _, file := range files {
		for _, pattern := range patterns {
			if releaseCheckMatchPath(pattern, file) {
				matched = append(matched, file)
				break
			}
		}
	}
	return matched
}

// releaseCheckMatchPath implements the subset of GitHub's `paths` globbing this
// repository uses: `**` matches across path separators, `*` and `?` match
// within a single segment.
func releaseCheckMatchPath(pattern, path string) bool {
	return releaseCheckMatchSegments(strings.Split(pattern, "/"), strings.Split(path, "/"))
}

func releaseCheckMatchSegments(pattern, path []string) bool {
	if len(pattern) == 0 {
		return len(path) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(path); i++ {
			if releaseCheckMatchSegments(pattern[1:], path[i:]) {
				return true
			}
		}
		return false
	}
	if len(path) == 0 {
		return false
	}
	if !releaseCheckMatchSegment(pattern[0], path[0]) {
		return false
	}
	return releaseCheckMatchSegments(pattern[1:], path[1:])
}

func releaseCheckMatchSegment(pattern, segment string) bool {
	if pattern == "" {
		return segment == ""
	}
	switch pattern[0] {
	case '*':
		// `*` never crosses a separator, so it can only consume the rest of
		// this segment.
		for i := 0; i <= len(segment); i++ {
			if releaseCheckMatchSegment(pattern[1:], segment[i:]) {
				return true
			}
		}
		return false
	case '?':
		if segment == "" {
			return false
		}
		return releaseCheckMatchSegment(pattern[1:], segment[1:])
	default:
		if segment == "" || segment[0] != pattern[0] {
			return false
		}
		return releaseCheckMatchSegment(pattern[1:], segment[1:])
	}
}

func releaseCheckReadFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s %v", releaseCheckContractPrefix, err)
	}
	return contents
}

// releaseCheckParseWorkflow parses a workflow file as a YAML node tree, so the
// `on:` key is read as the literal scalar GitHub writes rather than as the
// boolean a YAML 1.1 resolver would make of it.
func releaseCheckParseWorkflow(t *testing.T, path string, contents []byte) *yaml.Node {
	t.Helper()
	var doc yaml.Node
	if err := yaml.Unmarshal(contents, &doc); err != nil {
		t.Fatalf("%s %s is not parseable: %v", releaseCheckContractPrefix, path, err)
	}
	return &doc
}

func releaseCheckMappingValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func releaseCheckScalar(doc *yaml.Node, key string) string {
	node := releaseCheckMappingValue(doc.Content[0], key)
	if node == nil {
		return ""
	}
	return node.Value
}

// releaseCheckTriggerPaths returns the `paths` a workflow declares for an event
// trigger, or nil when it declares none.
func releaseCheckTriggerPaths(t *testing.T, path string, doc *yaml.Node, event string) []string {
	t.Helper()
	on := releaseCheckMappingValue(doc.Content[0], "on")
	if on == nil {
		t.Fatalf("%s %s declares no `on:` block", releaseCheckContractPrefix, path)
	}
	eventNode := releaseCheckMappingValue(on, event)
	if eventNode == nil {
		t.Fatalf("%s %s does not trigger on %s", releaseCheckContractPrefix, path, event)
	}
	pathsNode := releaseCheckMappingValue(eventNode, "paths")
	if pathsNode == nil {
		return nil
	}
	var paths []string
	for _, item := range pathsNode.Content {
		paths = append(paths, item.Value)
	}
	return paths
}

// releaseCheckReleasePleaseToken returns the `token:` the release-please action
// step receives (empty means it uses the runner's GITHUB_TOKEN).
func releaseCheckReleasePleaseToken(t *testing.T, dir string) string {
	t.Helper()
	raw := releaseCheckReadFile(t, filepath.Join(dir, releaseCheckReleasePleaseWorkflowPath))
	workflow := releaseCheckParseWorkflow(t, releaseCheckReleasePleaseWorkflowPath, raw)
	jobs := releaseCheckMappingValue(workflow.Content[0], "jobs")
	job := releaseCheckMappingValue(jobs, "release-please")
	if job == nil {
		t.Fatalf("%s %s has no release-please job", releaseCheckContractPrefix, releaseCheckReleasePleaseWorkflowPath)
	}
	for _, step := range releaseCheckMappingValue(job, "steps").Content {
		uses := releaseCheckMappingValue(step, "uses")
		if uses == nil || !strings.Contains(uses.Value, "release-please-action") {
			continue
		}
		with := releaseCheckMappingValue(step, "with")
		if with == nil {
			return ""
		}
		if token := releaseCheckMappingValue(with, "token"); token != nil {
			return token.Value
		}
		return ""
	}
	t.Fatalf("%s %s no longer uses release-please-action; the release PR (and therefore the approval-gated runs) "+
		"no longer exists", releaseCheckContractPrefix, releaseCheckReleasePleaseWorkflowPath)
	return ""
}

// releaseCheckAutoMergeStepRun returns the `run` block of the step that merges
// the release PR — the step that would fail if a check context these workflows
// report were ever made required.
func releaseCheckAutoMergeStepRun(t *testing.T, dir string) string {
	t.Helper()
	raw := releaseCheckReadFile(t, filepath.Join(dir, releaseCheckReleasePleaseWorkflowPath))
	workflow := releaseCheckParseWorkflow(t, releaseCheckReleasePleaseWorkflowPath, raw)
	jobs := releaseCheckMappingValue(workflow.Content[0], "jobs")
	job := releaseCheckMappingValue(jobs, "release-please")
	if job == nil {
		t.Fatalf("%s %s has no release-please job", releaseCheckContractPrefix, releaseCheckReleasePleaseWorkflowPath)
	}
	for _, step := range releaseCheckMappingValue(job, "steps").Content {
		run := releaseCheckMappingValue(step, "run")
		if run == nil || !strings.Contains(run.Value, "gh pr merge") {
			continue
		}
		return run.Value
	}
	t.Fatalf("%s %s has no step that merges the release PR with `gh pr merge`; the release chain and the stall the "+
		"acceptance notes warn about are both described in terms of that step",
		releaseCheckContractPrefix, releaseCheckReleasePleaseWorkflowPath)
	return ""
}

// releaseCheckMirroredContexts extracts the branch-protection contexts the
// auto-merge step would satisfy: a check-run (`-f name=`) or a commit status
// (`-f context=`). This repository mirrors none, so the extraction is what
// proves the empty expectation in the contract.
func releaseCheckMirroredContexts(stepRun string) []string {
	checkRun := regexp.MustCompile(`-f name='([^']*)'`).FindAllStringSubmatch(stepRun, -1)
	commitStatus := regexp.MustCompile(`-f context='([^']*)'`).FindAllStringSubmatch(stepRun, -1)
	seen := map[string]bool{}
	var contexts []string
	for _, match := range append(checkRun, commitStatus...) {
		if !seen[match[1]] {
			seen[match[1]] = true
			contexts = append(contexts, match[1])
		}
	}
	return contexts
}
