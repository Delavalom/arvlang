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

// read_file of a FIFO used to block in open forever (no writer), deaf to
// cancellation. It must fail fast: only regular files are read.
func TestReadFileRejectsFIFO(t *testing.T) {
	dir := t.TempDir()
	fifo := filepath.Join(dir, "pipe")
	if err := syscall.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}
	done := make(chan value.Object, 1)
	go func() {
		got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, fifo), grant(t, runtime.FSRead, dir))
		done <- got
	}()
	select {
	case got := <-done:
		wantErrorHas(t, got, "not a regular file")
	case <-time.After(5 * time.Second):
		// Unblock the stuck open (O_RDWR on a FIFO never blocks) before failing.
		if f, err := os.OpenFile(fifo, os.O_RDWR, 0); err == nil {
			f.Close()
		}
		t.Fatal("read_file on a FIFO blocked")
	}
}

func TestReadFileRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	got, _ := runWith(t, fmt.Sprintf(`read_file(%q)`, dir), grant(t, runtime.FSRead, dir))
	wantErrorHas(t, got, "not a regular file")
}

// openRegular keeps O_NOFOLLOW: a symlink planted at the leaf after the
// capability check is refused, even one pointing at a regular file.
func TestOpenRegularRefusesFinalSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(target, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if f, err := openRegular(link); err == nil {
		f.Close()
		t.Fatal("openRegular followed a symlink")
	}
	f, err := openRegular(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// The descriptor is back in blocking mode for the read.
	data, err := readCapped(f)
	if err != nil || string(data) != "x" {
		t.Fatalf("read = %q, %v", data, err)
	}
}
