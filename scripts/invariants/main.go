// Command invariants counts the tests that name each Arv invariant (I1 to
// I8, defined in docs/superpowers/plans/2026-11-09-arv-runtime/01-design.md
// §2). Tests name the invariant they protect in a comment, such as
// "// I7: a replay performs no live effect".
//
//	go run ./scripts/invariants          # a Markdown table of counts per package
//	go run ./scripts/invariants -check   # exit 1 if an invariant has no test
//
// docs/invariants.md holds the traceability matrix this feeds
// (roadmap 02-verification.md §V0).
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Invariants are the invariant names, in order.
var Invariants = []string{"I1", "I2", "I3", "I4", "I5", "I6", "I7", "I8"}

// tagRE finds invariant names inside a // comment. The name must stand
// alone (I7, not I70 or AI7).
var tagRE = regexp.MustCompile(`\bI[1-8]\b`)

// CountLine returns the invariants a line of Go names in its // comment.
// Each invariant counts at most once per line.
func CountLine(line string) []string {
	i := strings.Index(line, "//")
	if i < 0 {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, m := range tagRE.FindAllString(line[i:], -1) {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

// Count reads Go test source and adds the lines naming each invariant to
// counts.
func Count(r io.Reader, counts map[string]int) error {
	s := bufio.NewScanner(r)
	s.Buffer(make([]byte, 1024*1024), 16*1024*1024)
	for s.Scan() {
		for _, inv := range CountLine(s.Text()) {
			counts[inv]++
		}
	}
	return s.Err()
}

// Walk counts every *_test.go file under root, per package directory.
func Walk(root string) (map[string]map[string]int, error) {
	byPkg := map[string]map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			// scripts/ holds this tool, whose own tests name invariants as data.
			if path != root && (strings.HasPrefix(name, ".") || name == "testdata" || name == "dist" || name == "scripts") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()
		dir, _ := filepath.Rel(root, filepath.Dir(path))
		if byPkg[dir] == nil {
			byPkg[dir] = map[string]int{}
		}
		return Count(f, byPkg[dir])
	})
	return byPkg, err
}

// Totals sums the per-package counts.
func Totals(byPkg map[string]map[string]int) map[string]int {
	t := map[string]int{}
	for _, c := range byPkg {
		for k, v := range c {
			t[k] += v
		}
	}
	return t
}

// Missing returns the invariants with no test, in order.
func Missing(totals map[string]int) []string {
	var out []string
	for _, inv := range Invariants {
		if totals[inv] == 0 {
			out = append(out, inv)
		}
	}
	return out
}

func main() {
	check := flag.Bool("check", false, "exit 1 if an invariant has no test")
	root := flag.String("root", ".", "repository root")
	flag.Parse()

	byPkg, err := Walk(*root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "invariants:", err)
		os.Exit(2)
	}
	totals := Totals(byPkg)
	if *check {
		if m := Missing(totals); len(m) > 0 {
			fmt.Fprintf(os.Stderr, "invariants with no test: %s\n", strings.Join(m, ", "))
			os.Exit(1)
		}
		for _, inv := range Invariants {
			fmt.Printf("%s %d\n", inv, totals[inv])
		}
		return
	}

	pkgs := make([]string, 0, len(byPkg))
	for p := range byPkg {
		pkgs = append(pkgs, p)
	}
	sort.Strings(pkgs)
	fmt.Printf("| Package | %s |\n|---|%s\n", strings.Join(Invariants, " | "), strings.Repeat("---|", len(Invariants)))
	for _, p := range pkgs {
		row := make([]string, len(Invariants))
		any := false
		for i, inv := range Invariants {
			row[i] = fmt.Sprint(byPkg[p][inv])
			any = any || byPkg[p][inv] > 0
		}
		if any {
			fmt.Printf("| `%s` | %s |\n", filepath.ToSlash(p), strings.Join(row, " | "))
		}
	}
	row := make([]string, len(Invariants))
	for i, inv := range Invariants {
		row[i] = fmt.Sprint(totals[inv])
	}
	fmt.Printf("| **total** | %s |\n", strings.Join(row, " | "))
}
