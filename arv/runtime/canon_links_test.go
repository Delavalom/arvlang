package runtime

import (
	"fmt"
	"os"
	"strings"
	"testing"
)

// linkChain makes n symlinks in a fresh directory, each pointing to the
// next and the last to a directory end, every target padded with slashes
// to pad bytes, and returns the first link and end.
func linkChain(t *testing.T, n, pad int) (first, end string) {
	t.Helper()
	dir := realTempDir(t)
	end = dir + "/end"
	if err := os.Mkdir(end, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := n - 1; i >= 0; i-- {
		next := fmt.Sprintf("%s/l%d", dir, i+1)
		if i == n-1 {
			next = end
		}
		if len(next) > pad {
			t.Skipf("temp dir too long for %d-byte targets: %s", pad, dir)
		}
		cut := strings.LastIndexByte(next, '/')
		target := next[:cut] + strings.Repeat("/", pad-len(next)) + next[cut:]
		if err := os.Symlink(target, fmt.Sprintf("%s/l%d", dir, i)); err != nil {
			t.Fatal(err)
		}
	}
	return dir + "/l0", end
}

// CanonicalCharged charges at least what it allocates where single calls
// (TestCanonicalChargesWhatItAllocates) leave too little to see: the
// walk's buffer and its result for a long path missing near its root, the
// buffer growing past a short link to a long target, and Readlink's
// buffers for maxSymlinkHops links whose targets are just under, at and
// just over its buffer sizes. Each runs many times, so what is not
// charged adds up past the slack allowed for the run.
func TestCanonicalChargesAddUp(t *testing.T) {
	dir := realTempDir(t)
	if err := os.Symlink(dir+"/gone/"+strings.Repeat("b/", 420), dir+"/l"); err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"missing near the root":   dir + "/nothere/" + strings.Repeat("a", 4095-len(dir)-9),
		"short link, long target": dir + "/l/x",
	}
	for _, pad := range []int{255, 256, 511, 512, 1023} {
		first, _ := linkChain(t, maxSymlinkHops, pad)
		cases[fmt.Sprintf("%d links of %d bytes", maxSymlinkHops, pad)] = first + "/x"
	}
	for name, path := range cases {
		var charged int64
		used := heapDuring(func() {
			for i := 0; i < 100; i++ {
				CanonicalCharged(path, func(n int64) error { charged += n; return nil })
			}
		})
		if used > uint64(charged)+16<<10 {
			t.Errorf("%s: 100 walks allocated %d bytes, charged %d", name, used, charged)
		}
	}
}
