//go:build unix

package evaluator

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// writeBounded runs write_file(path, "x") holding fs_write(grantDir) and
// fails the test if it blocks.
func writeBounded(t *testing.T, path, grantDir string) value.Object {
	t.Helper()
	done := make(chan value.Object, 1)
	go func() {
		got, _ := runWith(t, fmt.Sprintf(`write_file(%q, "x")`, path), grant(t, runtime.FSWrite, grantDir))
		done <- got
	}()
	select {
	case got := <-done:
		return got
	case <-time.After(5 * time.Second):
		// Unblock the stuck open (O_RDWR on a FIFO never blocks) before failing.
		if f, err := os.OpenFile(path, os.O_RDWR, 0); err == nil {
			f.Close()
		}
		t.Fatalf("write_file(%s) blocked", path)
		return nil
	}
}

// Opening a FIFO for writing blocks until a reader appears, deaf to
// cancellation, so a scope could never finish. write_file refuses
// anything that is not a regular file, without blocking.
func TestWriteFileRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	wantErrorHas(t, writeBounded(t, fifo, dir), "write_file: ")

	// With a reader the open succeeds, so the fstat check must refuse it,
	// and nothing reaches the reader.
	r, err := os.OpenFile(fifo, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	wantErrorHas(t, writeBounded(t, fifo, dir), "not a regular file (named pipe)")
	if n, _ := r.Read(make([]byte, 8)); n != 0 {
		t.Fatalf("reader got %d bytes", n)
	}
}

func TestWriteFileRejectsDevice(t *testing.T) {
	wantErrorHas(t, writeBounded(t, "/dev/null", "/dev"), "not a regular file (device)")
}

// Regular files keep the old semantics: created if missing, truncated if
// longer, and a symlink at the leaf is refused (O_NOFOLLOW).
func TestWriteFileRegularSemantics(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if got := writeBounded(t, f, dir); got != NIL {
		t.Fatalf("create: %s", got.Inspect())
	}
	os.WriteFile(f, []byte("much longer content"), 0o644)
	if got := writeBounded(t, f, dir); got != NIL {
		t.Fatalf("overwrite: %s", got.Inspect())
	}
	if data, _ := os.ReadFile(f); string(data) != "x" {
		t.Fatalf("file = %q, want truncated to \"x\"", data)
	}
	if err := writeNoFollow(filepath.Join(dir, "link"), nil); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(dir, "link"))
	os.Symlink(f, filepath.Join(dir, "link"))
	if err := writeNoFollow(filepath.Join(dir, "link"), []byte("y")); err == nil {
		t.Fatal("writeNoFollow followed a symlink")
	}
}
