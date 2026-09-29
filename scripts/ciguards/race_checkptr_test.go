package ciguards

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/ovumcy/ovumcy-web/scripts/workflowfile"
)

const checkptrFlag = "-gcflags='modernc.org/...=-d=checkptr=0'"

var (
	goTestRaceRe = regexp.MustCompile(`\bgo test\b.*\s-race\b`)
)

// RaceRunsMissingCheckptrFlag returns "line N" for every uncommented `go test`
// command carrying -race whose command (backslash continuations joined) lacks
// the exact checkptr flag, plus the total of race commands seen. It reads run
// lines and block-scalar lines alike, since both are plain lines of text.
func RaceRunsMissingCheckptrFlag(content string) (missing []string, total int) {
	lines := strings.Split(content, "\n")
	for i := 0; i < len(lines); i++ {
		start := i + 1
		command := lines[i]
		if strings.HasPrefix(strings.TrimSpace(command), "#") {
			continue
		}
		for strings.HasSuffix(strings.TrimRight(command, " \t\r"), "\\") && i+1 < len(lines) {
			i++
			command += " " + lines[i]
		}
		if !goTestRaceRe.MatchString(command) {
			continue
		}
		total++
		if !strings.Contains(command, checkptrFlag) {
			missing = append(missing, "line "+strconv.Itoa(start))
		}
	}
	return missing, total
}

// WEB-102: without the flag a new race lane re-enables checkptr inside the
// transpiled SQLite driver (~10% race CPU). Every workflow is swept, so a race
// lane added later is covered without an edit here.
func TestEveryRaceRunSkipsCheckptrForModerncOnly(t *testing.T) {
	var all []string
	total := 0
	for _, workflow := range allWorkflowFiles(t) {
		missing, count := RaceRunsMissingCheckptrFlag(workflowfile.Read(t, workflow))
		total += count
		for _, m := range missing {
			all = append(all, filepath.Base(workflow)+":"+strings.TrimPrefix(m, "line "))
		}
	}
	if total == 0 {
		t.Fatal("found zero `go test -race` lines across every workflow — the scan is broken, since ci.yml runs several race lanes")
	}
	if len(all) > 0 {
		t.Fatalf("%d of %d `go test -race` run(s) lack %s (file:line): %v", len(all), total, checkptrFlag, all)
	}
}

func TestRaceRunsMissingCheckptrFlagCatchesEachShape(t *testing.T) {
	src := "a: 1\n" +
		"  # go test -race ./x\n" +
		"  run: go test -race -gcflags='modernc.org/...=-d=checkptr=0' ./a\n" +
		"  run: go test -race ./b\n" +
		"  run: |\n" +
		"    go test -race \\\n" +
		"      -timeout 20m ./c\n" +
		"    go test -list . ./d\n"
	missing, total := RaceRunsMissingCheckptrFlag(src)
	if total != 3 || len(missing) != 2 || missing[0] != "line 4" || missing[1] != "line 6" {
		t.Fatalf("total=%d missing=%v", total, missing)
	}
}
