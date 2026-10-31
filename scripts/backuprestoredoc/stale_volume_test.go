package backuprestoredoc

import (
	"fmt"
	"testing"
	"time"
)

// TestIsStaleEphemeralVolumeTouchesOnlyThisGuardsOldVolumes pins the one
// decision the stale sweep makes. It needs no docker: the sweep deletes by
// name alone, so the name parser is the whole safety boundary — a wrong
// verdict either deletes a volume a concurrent run is using or deletes
// nothing, and neither would fail any other test in the package.
func TestIsStaleEphemeralVolumeTouchesOnlyThisGuardsOldVolumes(t *testing.T) {
	now := time.Date(2026, 10, 31, 20, 0, 0, 0, time.UTC)
	named := func(createdAt time.Time) string {
		return fmt.Sprintf("%s%d%s", ephemeralVolumePrefix, createdAt.UnixNano(), ephemeralVolumeSuffix)
	}

	for name, tc := range map[string]struct {
		volume string
		stale  bool
	}{
		"older than the age is stale":          {named(now.Add(-staleEphemeralVolumeAge - time.Second)), true},
		"exactly the age is stale":             {named(now.Add(-staleEphemeralVolumeAge)), true},
		"younger than the age is kept":         {named(now.Add(-staleEphemeralVolumeAge + time.Second)), false},
		"just created is kept":                 {named(now), false},
		"the operator's data volume is kept":   {dataVolumeName, false},
		"an anonymous volume is kept":          {"0cb9eaada82745cc515c61eb6ba2d25ead7757fde72f35832ed4dc3be7cb09e3", false},
		"a prefix match without nanos":         {ephemeralVolumePrefix + ephemeralVolumeSuffix, false},
		"a prefix match with a foreign middle": {ephemeralVolumePrefix + "v2" + ephemeralVolumeSuffix, false},
		"the suffix alone":                     {"1testdata", false},
	} {
		t.Run(name, func(t *testing.T) {
			if got := isStaleEphemeralVolume(tc.volume, now); got != tc.stale {
				t.Fatalf("isStaleEphemeralVolume(%q) = %v, want %v", tc.volume, got, tc.stale)
			}
		})
	}
}
