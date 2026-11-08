//go:build !linux

package security

// CheckCalendarFeedFenceMounted accepts every directory outside Linux. The
// check reads st_dev and /proc/self/mountinfo, which only Linux has, and the
// one place CALENDAR_FEED_FENCE_REQUIRE_MOUNT is set is the Linux container
// image; a bare binary on another platform keeps its usual fence behaviour.
func CheckCalendarFeedFenceMounted(string) error {
	return nil
}
