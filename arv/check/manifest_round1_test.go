package check

import (
	"testing"
)

// A path that fails to canonicalise inside a grant (".." after a missing
// component) is not reported as uncovered: Outside says it could not
// resolve it, and why.
func TestOutsideTellsUnresolvedFromUncovered(t *testing.T) {
	dir := t.TempDir()
	r, err := Source(`read_file("missing/../x"); read_file("/etc/hosts");`)
	if err != nil {
		t.Fatal(err)
	}
	grants, err := Grants([]Entry{{1, "fs_read=."}}, dir)
	if err != nil {
		t.Fatal(err)
	}
	got := Outside(r, grants, dir)
	if len(got) != 2 || got[0].Scope != "missing/../x" || got[0].Err == nil ||
		got[1].Scope != "/etc/hosts" || got[1].Err != nil {
		t.Fatalf("Outside = %+v", got)
	}
}
