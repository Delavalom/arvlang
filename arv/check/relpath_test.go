package check

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
)

// A relative path is resolved against the working directory before the
// runtime checks its length, so one that is short as written but too long
// once resolved is never allowed: a note, not a need (which would print a
// grant --allow refuses, and fail a header that covers every path). The
// verdict holds only for this directory: from a shorter one the same path
// fits, so the note says so. An absolute path too long is never allowed
// from anywhere, and its note does not mention the directory.
func TestRelativePathTooLongOnceResolvedIsANote(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fits := strings.Repeat("a", runtime.MaxScopeLen-len(cwd)-1) // cwd + "/" + fits is the limit
	over := strings.Repeat("b", runtime.MaxScopeLen-len(cwd))   // one byte past it
	abs := "/" + strings.Repeat("c", runtime.MaxScopeLen)
	r, err := Source(fmt.Sprintf(`read_file(%q); fs_write(%q); read_file(%q);`, fits, over, abs))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Needs) != 1 || r.Needs[0].Scope != fits {
		t.Errorf("Needs = %d needs, want only fs_read of the %d-byte path", len(r.Needs), len(fits))
	}
	want := []string{
		fmt.Sprintf("fs_write at 1:%d: never allowed from this directory: fs_write: scope is %d bytes long once resolved (max %d)",
			len(fits)+16, runtime.MaxScopeLen+1, runtime.MaxScopeLen),
		fmt.Sprintf("read_file at 1:%d: never allowed: fs_read: scope is %d bytes long (max %d)",
			len(fits)+len(over)+30, runtime.MaxScopeLen+1, runtime.MaxScopeLen),
	}
	if len(r.Notes) != len(want) {
		t.Fatalf("Notes = %.300v, want %d notes", r.Notes, len(want))
	}
	for i, n := range r.Notes {
		if n.String() != want[i] {
			t.Errorf("Notes[%d] = %.300s, want %.300s", i, n.String(), want[i])
		}
	}
}
