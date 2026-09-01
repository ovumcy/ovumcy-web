package examplecompose

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wantFenceMount is the directory the shipped image points
// CALENDAR_FEED_FENCE_PATH into (see the Dockerfile). A stack that does not
// mount a persistent volume there leaves the fence on the read-only layer,
// which the app answers by disarming every armed calendar feed on each start.
const wantFenceMount = "/app/fence"

// fenceVolumeName is deliberately its OWN volume. The mechanism only works
// while the fence stays out of whatever a database backup captures, so a stack
// that pointed this at the data volume would ship a fence that is restored
// together with the database it is supposed to judge.
const fenceVolumeName = "ovumcy_fence"

var (
	ovumcyImage        = regexp.MustCompile(`image:\s*\S*ghcr\.io/ovumcy/ovumcy-web`)
	fenceVolumeMount   = regexp.MustCompile(`(?m)^\s*-\s*` + fenceVolumeName + `:(\S+)\s*$`)
	fenceVolumeDeclare = regexp.MustCompile(`(?m)^\s{2}` + fenceVolumeName + `:\s*$`)
)

// TestEveryBundledStackMountsTheCalendarFeedFence asserts that every shipped
// compose file running the ovumcy image mounts the fence volume at the path the
// image expects, and declares that volume.
//
// It exists because the fence was added to six compose files by hand. A seventh
// stack, or an edit that drops the mount from one, would ship a deployment
// whose calendar feed fails closed on every boot — and the only signal is a log
// line in a stack no test runs. A class fixed at N of N+1 sites is a new defect,
// so the N is derived here rather than trusted.
//
// It fails closed: finding no candidate file is itself a failure, so a renamed
// or moved stack cannot quietly stop being checked.
func TestEveryBundledStackMountsTheCalendarFeedFence(t *testing.T) {
	root := repoRoot(t)

	candidates := []string{filepath.Join(root, "docker-compose.yml")}
	err := filepath.WalkDir(filepath.Join(root, "docs", "examples"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && entry.Name() == "docker-compose.yml" {
			candidates = append(candidates, path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk docs/examples: %v", err)
	}

	var checked int
	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		if !ovumcyImage.Match(content) {
			continue
		}
		checked++
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)

		matches := fenceVolumeMount.FindAllSubmatch(content, -1)
		if len(matches) == 0 {
			t.Errorf("%s: runs the ovumcy image but mounts no %s volume, so the calendar-feed restore fence has nowhere to live and every armed feed is disarmed on each start", rel, fenceVolumeName)
			continue
		}
		for _, match := range matches {
			if got := string(match[1]); got != wantFenceMount {
				t.Errorf("%s: %s mounted at %q, want %q (the shipped image points CALENDAR_FEED_FENCE_PATH inside %s)", rel, fenceVolumeName, got, wantFenceMount, wantFenceMount)
			}
		}
		if !fenceVolumeDeclare.Match(content) {
			t.Errorf("%s: mounts %s without declaring it under the top-level volumes key", rel, fenceVolumeName)
		}
	}

	if checked < 2 {
		t.Fatalf("only %d compose file(s) running the ovumcy image were found: the guard is not reaching the stacks it is about", checked)
	}
}

// TestFenceMountPatternsDistinguishTheRegression proves the patterns above tell
// a correct stack from the two ways one goes wrong, on fixtures this test owns
// rather than on the shipped files it judges.
func TestFenceMountPatternsDistinguishTheRegression(t *testing.T) {
	good := strings.Join([]string{
		"services:",
		"  ovumcy:",
		"    image: ghcr.io/ovumcy/ovumcy-web:v2.0.0",
		"    volumes:",
		"      - ovumcy_data:/app/data",
		"      - ovumcy_fence:/app/fence",
		"volumes:",
		"  ovumcy_data:",
		"  ovumcy_fence:",
		"",
	}, "\n")
	if !ovumcyImage.MatchString(good) {
		t.Fatal("the image pattern must match a shipped stack, or the guard skips every file")
	}
	if match := fenceVolumeMount.FindStringSubmatch(good); match == nil || match[1] != wantFenceMount {
		t.Fatalf("the mount pattern must read the path back, got %v", match)
	}
	if !fenceVolumeDeclare.MatchString(good) {
		t.Fatal("the declaration pattern must match a declared volume")
	}

	// The fence pointed at the data volume's directory: mounted, declared, and
	// useless, because a database backup then carries it.
	insideTheDataVolume := strings.Replace(good, "ovumcy_fence:/app/fence", "ovumcy_fence:/app/data/fence", 1)
	if match := fenceVolumeMount.FindStringSubmatch(insideTheDataVolume); match == nil || match[1] == wantFenceMount {
		t.Fatalf("the mount pattern must report a wrong path rather than matching nothing, got %v", match)
	}

	// The mount kept, the declaration dropped: compose would refuse the stack,
	// and the guard has to name that rather than pass it.
	undeclared := strings.Replace(good, "\n  ovumcy_fence:\n", "\n", 1)
	if fenceVolumeDeclare.MatchString(undeclared) {
		t.Fatal("the declaration pattern must not match a stack whose top-level volume was removed")
	}
}
