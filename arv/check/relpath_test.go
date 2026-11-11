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
// grant --allow refuses, and fail a header that covers every path).
func TestRelativePathTooLongOnceResolvedIsANote(t *testing.T) {
	t.Chdir(t.TempDir())
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	fits := strings.Repeat("a", runtime.MaxScopeLen-len(cwd)-1) // cwd + "/" + fits is the limit
	over := strings.Repeat("b", runtime.MaxScopeLen-len(cwd))   // one byte past it
	r, err := Source(fmt.Sprintf(`read_file(%q); fs_write(%q);`, fits, over))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Needs) != 1 || r.Needs[0].Scope != fits {
		t.Errorf("Needs = %d needs, want only fs_read of the %d-byte path", len(r.Needs), len(fits))
	}
	want := fmt.Sprintf("fs_write at 1:%d: never allowed: fs_write: scope is %d bytes long (max %d)",
		len(fits)+16, runtime.MaxScopeLen+1, runtime.MaxScopeLen)
	if len(r.Notes) != 1 || r.Notes[0].String() != want {
		t.Errorf("Notes = %v, want [%s]", r.Notes, want)
	}
}
