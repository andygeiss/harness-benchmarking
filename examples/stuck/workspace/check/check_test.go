package check

import (
	"testing"
	"time"
)

// TestYear is unsatisfiable by design: no source you can write in this package
// makes time.Now() report 1999. The example exists to drive the harness into a
// no-progress state and confirm the stagnation guard (-max-stale) stops the run.
func TestYear(t *testing.T) {
	if got := time.Now().Year(); got != 1999 {
		t.Fatalf("year = %d, want 1999 — this test cannot be made to pass", got)
	}
}
