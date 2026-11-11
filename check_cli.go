package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/check"
)

// checkCLI implements "arv check FILE": print the grants FILE can need, one
// "--allow kind=scope" line each, then a "# " line for each call whose
// need is not known before running. It never runs FILE. If FILE has an
// arv:allow header, a need outside it is an error (exit 1).
func checkCLI(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || strings.HasPrefix(args[0], "--") {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	file := args[0]
	src, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	r, err := checkSource(string(src), file)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	for _, n := range r.Needs {
		fmt.Fprintln(stdout, "--allow", n.Quoted())
	}
	for _, n := range r.Notes {
		fmt.Fprintln(stdout, "#", oneLine(n.String()))
	}
	entries, err := check.Manifest(string(src))
	if err != nil {
		fmt.Fprintf(stderr, "error: %s: %s\n", file, err)
		return 1
	}
	if entries == nil {
		return 0
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if err := checkManifest(r, entries, file, cwd); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// checkSource checks src, giving a parse error the file's name as run
// does.
func checkSource(src, file string) (*check.Report, error) {
	r, err := check.Source(src)
	var pe *check.ParseError
	if errors.As(err, &pe) {
		return nil, errors.New((&arv.ParseError{Errs: pe.Errs}).In(file))
	}
	return r, err
}

// checkManifest fails if r needs a grant outside the manifest entries.
func checkManifest(r *check.Report, entries []check.Entry, file, cwd string) error {
	grants, err := check.Grants(entries, cwd)
	if err != nil {
		return fmt.Errorf("%s: %w", file, err)
	}
	out := check.Outside(r, grants, cwd)
	if len(out) == 0 {
		return nil
	}
	needs := make([]string, len(out))
	for i, n := range out {
		needs[i] = fmt.Sprintf("%s (%s at %d:%d)", n.Quoted(), n.Fn, n.Line, n.Col)
	}
	return fmt.Errorf("%s needs %s, outside its arv:allow manifest", file, strings.Join(needs, ", "))
}

// oneLine replaces control characters, so a note is one line.
func oneLine(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return '?'
		}
		return r
	}, s)
}
