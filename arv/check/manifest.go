package check

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/delavalom/arvlang/arv/runtime"
)

// Entry is one "// arv:allow kind=scope" line of a program's header.
type Entry struct {
	Line int
	Text string // kind=scope, as --allow takes it
}

const directive = "arv:allow"

// Manifest returns the arv:allow entries of src's header: the lines
// before the first line of code, each blank or a // comment. A comment
// after the header is not read, even if it says arv:allow: it may be part
// of a string. Each entry is exactly one kind=scope, with no spaces.
func Manifest(src string) ([]Entry, error) {
	var entries []Entry
	for i, line := range strings.Split(src, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, "//") {
			break
		}
		c := strings.TrimSpace(line[2:])
		if !strings.HasPrefix(c, directive) {
			continue
		}
		rest := c[len(directive):]
		if rest != "" && rest[0] != ' ' && rest[0] != '\t' {
			continue // arv:allowed, say: not the directive
		}
		rest = strings.TrimSpace(rest)
		if rest == "" || strings.ContainsAny(rest, " \t") {
			return nil, fmt.Errorf("line %d: %s wants one kind=scope, got %q", i+1, directive, rest)
		}
		entries = append(entries, Entry{Line: i + 1, Text: rest})
	}
	return entries, nil
}

// Grants turns entries into grants exactly as --allow does
// (runtime.Parse): a relative path is resolved against cwd.
func Grants(entries []Entry, cwd string) ([]runtime.Capability, error) {
	caps := make([]runtime.Capability, 0, len(entries))
	for _, e := range entries {
		c, err := runtime.Parse(e.Text, cwd)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", e.Line, err)
		}
		caps = append(caps, c)
	}
	return caps, nil
}

// Outside returns the first site of each grant in r that grants do not
// cover, in source order. It decides as the runtime does, with
// runtime.CapSet.Admit, after resolving a relative path against cwd as
// the program would: like --allow, that reads filesystem metadata (lstat,
// readlink) to resolve symlinks, and nothing else.
func Outside(r *Report, grants []runtime.Capability, cwd string) []Need {
	set := runtime.NewCapSet(grants...)
	seen := map[string]bool{}
	var out []Need
	for _, s := range r.Sites {
		if seen[s.String()] {
			continue
		}
		seen[s.String()] = true
		scope := s.Scope
		if (s.Kind == runtime.FSRead || s.Kind == runtime.FSWrite) && !filepath.IsAbs(scope) {
			scope = runtime.Resolve(cwd, scope)
		}
		lit, err := runtime.Literal(s.Kind, scope)
		if err == nil {
			_, err = set.Admit(lit)
		}
		if err != nil {
			out = append(out, s)
		}
	}
	return out
}
