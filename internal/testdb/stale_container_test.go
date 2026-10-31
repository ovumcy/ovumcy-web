package testdb

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// TestIsStalePostgresContainerTouchesOnlyOldLabelledContainers pins the one
// decision the sweep makes. It needs no docker: the sweep deletes by the label
// value alone, so the parser is the whole safety boundary.
func TestIsStalePostgresContainerTouchesOnlyOldLabelledContainers(t *testing.T) {
	now := time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
	stamp := func(createdAt time.Time) string { return fmt.Sprintf("%d", createdAt.UnixNano()) }

	for name, tc := range map[string]struct {
		label string
		stale bool
	}{
		"older than the age is stale":      {stamp(now.Add(-staleContainerAge - time.Second)), true},
		"exactly the age is stale":         {stamp(now.Add(-staleContainerAge)), true},
		"younger than the age is kept":     {stamp(now.Add(-staleContainerAge + time.Second)), false},
		"just created is kept":             {stamp(now), false},
		"an empty value is kept":           {"", false},
		"a non-numeric value is kept":      {"true", false},
		"a signed value is not a stamp":    {"-1", false},
		"a plus-signed value neither":      {"+1", false},
		"a value with a suffix is kept":    {stamp(now.Add(-2*staleContainerAge)) + "x", false},
		"a value past int64 is kept":       {"99999999999999999999", false},
		"a fractional value is kept":       {"1.5", false},
		"a zero stamp is the epoch: stale": {"0", true},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isStalePostgresContainer(tc.label, now); got != tc.stale {
				t.Fatalf("isStalePostgresContainer(%q) = %v, want %v", tc.label, got, tc.stale)
			}
		})
	}
}

// TestRemoveStalePostgresContainersRemovesOnlyStaleListedIDs drives the sweep
// through the injected docker runner: only a stale labelled row is removed,
// with -f -v, and a malformed row is skipped.
func TestRemoveStalePostgresContainersRemovesOnlyStaleListedIDs(t *testing.T) {
	now := time.Now()
	old := fmt.Sprintf("%d", now.Add(-2*time.Hour).UnixNano())
	fresh := fmt.Sprintf("%d", now.Add(-time.Minute).UnixNano())
	listing := strings.Join([]string{"aaa " + old, "bbb " + fresh, "ccc", "ddd garbage", "eee " + old + " extra"}, "\n")

	var removed []string
	original := runDockerCommandWithError
	runDockerCommandWithError = func(args ...string) (string, error) {
		switch args[0] {
		case "ps":
			if !strings.Contains(strings.Join(args, " "), "label="+PostgresContainerLabel) {
				t.Errorf("the listing must filter on the label; got %v", args)
			}
			return listing, nil
		case "rm":
			removed = append(removed, strings.Join(args, " "))
			return "", nil
		}
		return "", fmt.Errorf("unexpected docker %v", args)
	}
	t.Cleanup(func() { runDockerCommandWithError = original })

	removeStalePostgresContainers()

	if len(removed) != 1 || removed[0] != "rm -f -v aaa" {
		t.Fatalf("expected only the stale container to be removed with -f -v, got %v", removed)
	}
}
