package main

import (
	"strings"
	"testing"
)

// --- fragment naming ---------------------------------------------------------

func TestFragmentPathForBranchDropsTheTypePrefix(t *testing.T) {
	t.Parallel()

	for headRef, want := range map[string]string{
		"fix/web123-cli-schema-check": "changelog.d/web123-cli-schema-check.md",
		"ci/web122-fragment-slug":     "changelog.d/web122-fragment-slug.md",
		"feat/area/detail":            "changelog.d/area-detail.md",
		"no-type-prefix":              "changelog.d/no-type-prefix.md",
	} {
		if got := fragmentPathForBranch(headRef); got != want {
			t.Errorf("fragmentPathForBranch(%q) = %q, want %q", headRef, got, want)
		}
	}
}

func TestCheckPassesWhenAnAddedFragmentCarriesTheBranchSlug(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/web122-fragment-slug.md", "### Internal\n\n- **Fragment names are checked.**\n")
	commitAll(t, dir, "ci: check the fragment name")

	failure, err := check(dir, "main", "ci/web122-fragment-slug", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure != "" {
		t.Fatalf("expected a fragment named after the branch to pass, got:\n%s", failure)
	}
}

func TestCheckRefusesAFragmentNotNamedAfterTheBranch(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/boot-refuses-a-missing-email-index.md", "### Security\n\n- **A misnamed entry.**\n")
	commitAll(t, dir, "fix: a misnamed fragment")

	failure, err := check(dir, "main", "fix/web104-email-index-boot-check", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	for _, want := range []string{"FAILED", "changelog.d/web104-email-index-boot-check.md", "changelog.d/boot-refuses-a-missing-email-index.md"} {
		if !strings.Contains(failure, want) {
			t.Errorf("failure does not mention %q:\n%s", want, failure)
		}
	}
}

func TestCheckNamesTheExactFragmentWhenTheBranchAddsNone(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "internal/app.go", "package app\n")
	commitAll(t, dir, "fix: no fragment")

	failure, err := check(dir, "main", "fix/web200-something", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(failure, "adds no fragment") || !strings.Contains(failure, "Add changelog.d/web200-something.md ") {
		t.Fatalf("expected the missing-fragment failure to name changelog.d/web200-something.md, got:\n%s", failure)
	}
}

func TestCheckPointsAnUnreleasedEditAtTheBranchFragment(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "CHANGELOG.md", strings.Replace(fixtureChangelog,
		"- **A frozen entry.**", "- **A new entry typed into the backlog.**\n- **A frozen entry.**", 1))
	commitAll(t, dir, "fix: edit the backlog")

	failure, err := check(dir, "main", "fix/web200-something", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !strings.Contains(failure, "Put the entry in changelog.d/web200-something.md instead.") {
		t.Fatalf("expected the CHANGELOG.md refusal to name the branch fragment, got:\n%s", failure)
	}
}

func TestCheckDoesNotOweABranchFragmentForReleaseAssembly(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "CHANGELOG.md", strings.Replace(fixtureChangelog,
		"## [1.0.0] - 2026-01-01",
		"## [1.1.0] - 2026-02-02\n\n### Added\n\n- **An assembled release.**\n\n## [1.0.0] - 2026-01-01", 1))
	commitAll(t, dir, "chore: cut 1.1.0")

	failure, err := check(dir, "main", "chore/release-1.1.0", gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure != "" {
		t.Fatalf("expected release assembly to pass without a branch fragment, got:\n%s", failure)
	}
}

func TestResolveHeadRefPrefersTheCIHeadRef(t *testing.T) {
	dir := initRepo(t)

	got, err := resolveHeadRef(dir, " fix/from-ci ", gitOutput)
	if err != nil || got != "fix/from-ci" {
		t.Fatalf("resolveHeadRef with GITHUB_HEAD_REF = %q, %v, want fix/from-ci", got, err)
	}
	got, err = resolveHeadRef(dir, "", gitOutput)
	if err != nil || got != "feature" {
		t.Fatalf("resolveHeadRef on branch feature = %q, %v, want feature", got, err)
	}
}

// A detached HEAD outside CI names no branch, so the naming rule is skipped and
// any added fragment passes, as it did before the rule existed.
func TestCheckSkipsTheNamingRuleOnADetachedHeadWithoutAHeadRef(t *testing.T) {
	dir := initRepo(t)
	writeFile(t, dir, "changelog.d/any-name.md", "### Fixed\n\n- **Detached.**\n")
	commitAll(t, dir, "fix: detached")
	run(t, dir, "checkout", "-q", "--detach")

	headRef, err := resolveHeadRef(dir, "", gitOutput)
	if err != nil || headRef != "" {
		t.Fatalf("resolveHeadRef on a detached HEAD = %q, %v, want \"\"", headRef, err)
	}
	failure, err := check(dir, "main", headRef, gitOutput)
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if failure != "" {
		t.Fatalf("expected a detached HEAD to skip the naming rule, got:\n%s", failure)
	}
}
