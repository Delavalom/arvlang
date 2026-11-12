package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/check"
)

// splitManifest takes --manifest, a flag without a value, out of run's
// arguments. It reads flags as parseRun does, every other flag taking a
// value, so "--allow --manifest" stays a (bad) grant.
func splitManifest(args []string) (rest []string, manifest bool) {
	for len(args) > 0 && strings.HasPrefix(args[0], "--") {
		if args[0] == "--manifest" {
			manifest = true
			args = args[1:]
			continue
		}
		if len(args) < 2 {
			break
		}
		rest = append(rest, args[0], args[1])
		args = args[2:]
	}
	return append(rest, args...), manifest
}

// applyManifest makes opts grant exactly src's arv:allow header, and
// refuses (exit 1), before anything runs, a program that arv check finds
// needs a grant outside it. A need arv check cannot see (a note) is not
// refused: the run holds the header's grants and nothing else, so the
// runtime denies it if it is outside. A program that does not parse is
// left to arv.Run, which reports it as it does without --manifest. It
// returns 0 to go on running.
func applyManifest(opts *arv.Options, src, file, cwd string, stderr io.Writer) int {
	if len(opts.Caps) > 0 {
		fmt.Fprintln(stderr, "error: --manifest grants exactly the file's arv:allow lines; it cannot be combined with --allow")
		return 2
	}
	entries, err := check.Manifest(src)
	if err == nil {
		opts.Caps, err = check.Grants(entries, cwd)
	}
	if err != nil {
		fmt.Fprintf(stderr, "error: %s: %s\n", file, err)
		return 1
	}
	r, err := check.Source(src)
	var pe *check.ParseError
	if errors.As(err, &pe) {
		return 0
	}
	if err == nil {
		err = checkManifest(r, entries, file, cwd)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
