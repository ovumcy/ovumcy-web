package httpx

import "testing"

// TestProbeGateCascade is a deliberate failure, pushed on a throwaway branch to
// measure one thing: what branch protection reports for the required `test`
// context when `test-go` fails and `test` is therefore skipped through its
// `needs` edge. It is never merged and the branch is deleted after the reading.
func TestProbeGateCascade(t *testing.T) {
	t.Fatal("deliberate failure: measuring the required-check cascade")
}
