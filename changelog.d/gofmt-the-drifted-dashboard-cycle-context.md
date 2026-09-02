none

Formatting-only fix: `DashboardCycleContext`'s field block in
`internal/services/dashboard_cycle.go` had drifted out of gofmt alignment, so
`gofmt -l` over the tracked Go files named that one file on a clean `main`. No
CI lane runs gofmt, which is why it stayed. Whitespace only — no declaration,
type or comment changed.
