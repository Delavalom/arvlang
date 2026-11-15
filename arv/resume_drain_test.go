package arv

import (
	"os"
	"strings"
	"testing"
)

// Review round 4 (Critical): the cut log's last record is root's check
// that saw the 6 ms scope's deadline (by root:2). Using it drained the log
// before the replay fired that deadline, and in that window a live task's
// failure could become the scope's first failure. The resumed log then
// did not replay (its replay fires the deadline first), and printed that
// failure ("outerA") as the outer scope's. The log now counts as drained
// only once that deadline has fired, so the scope's failure is always its
// timeout, as the crashed run's record says.
func TestResumedLiveFailureWaitsForALoggedDeadline(t *testing.T) {
	src, err := os.ReadFile("testdata/resume/deadline.arv")
	if err != nil {
		t.Fatal(err)
	}
	cut, err := os.ReadFile("testdata/resume/deadline-cut.log")
	if err != nil {
		t.Fatal(err)
	}
	rounds := 60
	if testing.Short() {
		rounds = 10
	}
	bad := 0
	for range rounds {
		printed, resumed, serr := resumeRun(t, string(src), cut, nil)
		replayed, rerr := replayRun(t, string(src), resumed)
		if errText(rerr) != errText(serr) || replayed != printed {
			if bad++; bad == 1 {
				t.Logf("resume printed %q (%v); its replay printed %q (%v)", printed, serr, replayed, rerr)
			}
		}
		if !strings.Contains(printed, "budget exceeded: timeout (limit 6 ms)\n") {
			if bad++; bad == 1 {
				t.Logf("resume printed %q: a live failure came before the logged deadline", printed)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d bad resumes of %d", bad, rounds)
	}
}
