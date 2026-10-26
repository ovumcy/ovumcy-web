package releasegate

import (
	"path/filepath"
	"testing"
)

// checkoutRoot is the repository root as a runner hands it to a step in
// GITHUB_WORKSPACE, in the forward-slash form bash reads on every platform.
func checkoutRoot(t *testing.T) string {
	t.Helper()

	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve the repository root: %v", err)
	}
	return filepath.ToSlash(root)
}
