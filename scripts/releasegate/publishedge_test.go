package releasegate

import (
	"slices"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

// publishJob is the job that builds, signs and promotes the image. Every
// script case in this package judges the gate; none of them would notice the
// gate stop being in this job's way.
const publishJob = "publish"

// The two runner conditions that put the gate on the release path, written as
// they are pinned: whitespace and the `${{ }}` wrapper are the only freedom a
// rewrite has. The gate must run on every tag, and `publish` may pass a
// `skipped` gate only where the gate is skipped by design, off the tag path.
//
// The second conjunct of `publishCondition` is the tip job's: off the tag path
// `publish` runs only over that job's `success` AND its answer `true`, and a
// tag, where the job is skipped by design, passes on the ref type alone. Without
// the first half a failed tip read would publish; without the second a commit
// that is no longer the newest on `main` would enter the alias group and cancel
// a waiting newer one.
const (
	gateCondition    = "github.ref_type == 'tag'"
	tipJob           = "confirm-main-tip"
	publishCondition = "!cancelled() && (needs.verify-release-tag.result == 'success' || (needs.verify-release-tag.result == 'skipped' && github.ref_type != 'tag')) && (github.ref_type == 'tag' || (needs.confirm-main-tip.result == 'success' && needs.confirm-main-tip.outputs.is_tip == 'true'))"
)

// TestPublishWaitsOnTheTipJob pins the edge for the alias path: its condition
// reads the tip job's result and output, and a job it does not wait on has
// neither when the condition is evaluated.
func TestPublishWaitsOnTheTipJob(t *testing.T) {
	block := workflowfile.Job(t, gateWorkflow, publishJob)
	needs, err := workflowfile.JobNeeds(block)
	if err != nil {
		t.Fatalf("%s, job %q: `needs:` cannot be read (%v), so nothing shows the job waits on %q:\n%s",
			gateWorkflow, publishJob, err, tipJob, block)
	}
	if !slices.Contains(needs, tipJob) {
		t.Fatalf("%s, job %q needs %q, not %q: an older commit's publish would enter the alias group without the newest-commit check",
			gateWorkflow, publishJob, needs, tipJob)
	}
}

// TestPublishWaitsOnTheReleaseGate pins the edge itself. Without the need,
// `publish` starts alongside the gate rather than after it, and its `if:` reads
// the result of a job it does not wait on.
func TestPublishWaitsOnTheReleaseGate(t *testing.T) {
	block := workflowfile.Job(t, gateWorkflow, publishJob)
	needs, err := workflowfile.JobNeeds(block)
	if err != nil {
		t.Fatalf("%s, job %q: `needs:` cannot be read (%v), so nothing shows the job waits on %q:\n%s",
			gateWorkflow, publishJob, err, gateJob, block)
	}
	if !slices.Contains(needs, gateJob) {
		t.Fatalf("%s, job %q needs %q, not %q: a tag would be built, signed and promoted without waiting for the gate's verdict",
			gateWorkflow, publishJob, needs, gateJob)
	}
}

// TestPublishPassesATagOnlyOnTheGatesSuccess pins `publish`'s condition to the
// one form that lets a tag through on nothing but the gate's `success`.
// `always()` or a bare `!cancelled()` run it over a failed gate; dropping the
// ref-type conjunct runs it over a gate that skipped on a tag; and a form the
// shared reader cannot follow is refused as well, since it is a form nobody has
// checked.
func TestPublishPassesATagOnlyOnTheGatesSuccess(t *testing.T) {
	assertJobCondition(t, publishJob, publishCondition)
}

// TestTheReleaseGateRunsOnEveryTag pins the other half. A gate that skips on a
// tag is refused by `publish` — which is the only thing standing between that
// skip and an unchecked release — and a gate that also runs off the tag path
// asks for check runs the calling workflow is still producing.
func TestTheReleaseGateRunsOnEveryTag(t *testing.T) {
	assertJobCondition(t, gateJob, gateCondition)
}

func assertJobCondition(t *testing.T, job, want string) {
	t.Helper()

	block := workflowfile.Job(t, gateWorkflow, job)
	got, err := workflowfile.JobCondition(block)
	if err != nil {
		t.Fatalf("%s, job %q: the job-level `if:` cannot be read (%v); want exactly `${{ %s }}`:\n%s",
			gateWorkflow, job, err, want, block)
	}
	if workflowfile.WithoutSpaces(got) != workflowfile.WithoutSpaces(want) {
		t.Fatalf("%s, job %q: the job-level `if:` reads %q; want exactly `${{ %s }}`",
			gateWorkflow, job, workflowfile.WithoutSpaces(got), want)
	}
}
