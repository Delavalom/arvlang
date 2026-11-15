# Phase 8.1, task p81.9: Soundness property and fuzzing

> **For agentic workers:** this file is one task, p81.9. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p81.8 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (860a158) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `15-phase8-tooling.md` §8.1. Deviations from it, and why: `p81-plan-1.md`. Phases 5E, 6, 7 and 8.2 are built at the same time and also touch the CLI, parser and evaluator: change only the files this task names.

### Task p81.9: Soundness property and fuzzing

Two tests over the finished package. They pass when written; Step 2b shows that the property bites.

- `TestReportedGrantsSufficeAndAreNeeded` builds 300 random programs (seeded) that nest `scope`, scope options, `supervise: "collect"` and `"restart"`, `match`, `if`, function calls, `spawn ... with`, `await`, `for`, hash indexing around `getenv`, `read_file` and `write_file` leaves with literal arguments. Every wrapper runs its hole once and every spawn hands its task exactly the constructors of the leaves inside it. Running each program with exactly the grants `arv check` reports never meets a denial (soundness), and leaving any one out does (no reported grant is spare, since every leaf runs).
- `FuzzCheck` seeds from `examples/` and `e2e/testdata/`: `Source` and `Manifest` never panic, a report is the same every time, needs are sorted and distinct, and no grant prints as more than one line.

**Files:**
- Create (test): `arv/check/sound_test.go`, `arv/check/fuzz_test.go`

- [ ] **Step 1: Write the tests**

Create `arv/check/sound_test.go`:

```go
package check

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
)

// gen builds random programs in which every leaf effect runs: each
// wrapper evaluates its hole exactly once, and a spawn hands its task
// exactly the capabilities of the leaves inside it.
type gen struct {
	r     *rand.Rand
	leaf  int
	needs map[string]bool // the grants the leaves need, in --allow form
}

// leafExpr is one effect call with a literal argument, and its
// constructor for a spawn's with list.
func (g *gen) leafExpr() (call, constructor string) {
	g.leaf++
	n := g.r.Intn(4)
	switch g.r.Intn(3) {
	case 0:
		g.needs[fmt.Sprintf("env=ARV_P81_%d", n)] = true
		return fmt.Sprintf(`getenv("ARV_P81_%d")`, n), fmt.Sprintf(`env("ARV_P81_%d")`, n)
	case 1:
		g.needs[fmt.Sprintf("fs_read=r%d.txt", n)] = true
		return fmt.Sprintf(`read_file("r%d.txt")`, n), fmt.Sprintf(`fs_read("r%d.txt")`, n)
	default:
		g.needs[fmt.Sprintf("fs_write=w%d.txt", n)] = true
		return fmt.Sprintf(`write_file("w%d.txt", "x")`, n), fmt.Sprintf(`fs_write("w%d.txt")`, n)
	}
}

// expr returns an expression and the constructors of the leaves in it.
func (g *gen) expr(depth int) (string, []string) {
	if depth == 0 || g.r.Intn(4) == 0 {
		k := 1 + g.r.Intn(2)
		var calls, caps []string
		for i := 0; i < k; i++ {
			c, k := g.leafExpr()
			calls, caps = append(calls, c), append(caps, k)
		}
		return "len([" + strings.Join(calls, ", ") + "])", caps
	}
	h, caps := g.expr(depth - 1)
	with := "[" + strings.Join(caps, ", ") + "]"
	switch g.r.Intn(10) {
	case 0:
		return "scope { " + h + " }", caps
	case 1:
		return "scope (timeout_ms: 600000, steps: 100000000, tasks: 50) { " + h + " }", caps
	case 2:
		return "match scope (supervise: \"collect\") { await spawn (fn() { " + h + " })() with " + with +
			" } { ok(v) => v, err(e) => error(e[\"message\"]) }", caps
	case 3:
		return "if (true) { " + h + " } else { 0 }", caps
	case 4:
		return "(fn(x) { " + h + " })(1)", caps
	case 5:
		return "await spawn (fn() { " + h + " })() with " + with, caps
	case 6:
		return "len(for x in [1] { " + h + " })", caps
	case 7:
		return "{\"k\": " + h + "}[\"k\"]", caps
	case 8:
		return "scope (supervise: \"restart\", retries: 1, backoff_ms: 1) { await spawn (fn() { " + h + " })() with " + with + " }", caps
	default:
		return "match " + h + " { ok(v) => v, err(e) => error(e[\"message\"]) }", caps
	}
}

// Running a program with exactly the grants arv check reports never
// meets a capability denial (soundness), and leaving any one out does
// (every reported grant is used: these programs run every leaf).
func TestReportedGrantsSufficeAndAreNeeded(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for n := 0; n < 4; n++ {
		if err := os.WriteFile(fmt.Sprintf("r%d.txt", n), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := rand.New(rand.NewSource(81))
	for i := 0; i < 300; i++ {
		g := &gen{r: r, needs: map[string]bool{}}
		src, _ := g.expr(1 + r.Intn(4))
		src += ";"
		rep, err := Source(src)
		if err != nil {
			t.Fatalf("%s: %v", src, err)
		}
		if rep.Notes != nil {
			t.Fatalf("%s: notes %v", src, rep.Notes)
		}
		var got []Entry
		for _, n := range rep.Needs {
			got = append(got, Entry{Text: n.String()})
		}
		if len(got) != len(g.needs) {
			t.Fatalf("%s: check needs %v, the leaves %v", src, got, g.needs)
		}
		if err := runWithEntries(src, got, dir); err != nil {
			t.Fatalf("%s\nwith the reported grants: %v", src, err)
		}
		for drop := range got {
			rest := append(append([]Entry(nil), got[:drop]...), got[drop+1:]...)
			err := runWithEntries(src, rest, dir)
			if err == nil || !strings.Contains(err.Error(), "capability denied") {
				t.Fatalf("%s\nwithout %s: %v, want a denial", src, got[drop].Text, err)
			}
		}
	}
}

func runWithEntries(src string, entries []Entry, cwd string) error {
	caps, err := Grants(entries, cwd)
	if err != nil {
		return err
	}
	_, err = arv.Run(src, arv.Options{Caps: caps})
	return err
}
```

Create `arv/check/fuzz_test.go`:

```go
package check

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// FuzzCheck: arv check reads untrusted programs. It must never panic, must
// give the same report every time, and must print every grant as one line.
func FuzzCheck(f *testing.F) {
	for _, dir := range []string{"../../examples", "../../e2e/testdata"} {
		paths, _ := filepath.Glob(filepath.Join(dir, "*.arv"))
		for _, p := range paths {
			if src, err := os.ReadFile(p); err == nil {
				f.Add(string(src))
			}
		}
	}
	f.Add(`let u = "http://a" + "/"; http_get(u); spawn f() with [net("*"), fs_read("x")];`)
	f.Add("// arv:allow env=A\ngetenv(\"A\\n\");")
	f.Fuzz(func(t *testing.T, src string) {
		r, err := Source(src)
		if _, merr := Manifest(src); merr != nil && !strings.Contains(merr.Error(), "arv:allow wants") {
			t.Fatalf("Manifest: unexpected error %v", merr)
		}
		if err != nil {
			if _, ok := err.(*ParseError); !ok {
				t.Fatalf("Source: %v (not a parse error)", err)
			}
			return
		}
		again, err := Source(src)
		if err != nil || !reflect.DeepEqual(r, again) {
			t.Fatalf("Source is not deterministic:\n%+v\n%+v", r, again)
		}
		for i, n := range r.Needs {
			if q := n.Quoted(); strings.ContainsAny(q, "\n\r") {
				t.Fatalf("need %q prints as more than one line: %q", n.String(), q)
			}
			if i > 0 && rank(r.Needs[i-1].Kind) == rank(n.Kind) && r.Needs[i-1].Scope >= n.Scope {
				t.Fatalf("needs not sorted and distinct: %v", r.Needs)
			}
		}
		if len(r.Needs) > len(r.Sites) {
			t.Fatalf("%d needs from %d sites", len(r.Needs), len(r.Sites))
		}
	})
}
```

- [ ] **Step 2: Run them**

Run: `go test -race ./arv/check/`
Expected: `ok`. The code they test is already in place, so they pass at once.

- [ ] **Step 2b: Show that the property bites**

In `arv/check/walk.go`, delete the line `add(n.Body)` under `case *ast.FunctionLiteral:` and run `go test ./arv/check/ -run Reported`. Expected: `FAIL`, with a message like `(fn(x) { len([read_file("r1.txt")]) })(1);: check needs [], the leaves map[fs_read=r1.txt:true]`. Restore it: `git checkout arv/check/walk.go`.

- [ ] **Step 2c: Fuzz for 2 minutes**

Run: `go test ./arv/check/ -run '^$' -fuzz FuzzCheck -fuzztime 2m`
Expected: `PASS`. If it finds a crasher, Go saves it under `arv/check/testdata/fuzz/FuzzCheck/`. A bug in `arv/check` gets a fix, test first, and the crasher stays as a seed (commit it). A bug outside `arv/check` (the parser, say) gets a `t.Skip("BUG: ...")` test with the reproduction and a report. Record the fuzz time and the result for the notes.

- [ ] **Step 3: Check the whole tree**

Run: `gofmt -l .` (prints nothing), `go vet ./...` and `go test -race ./...`.

- [ ] **Step 4: Commit**

```bash
git add arv/check
git commit -m "test(check): soundness property over random programs, and a fuzz target" -m "Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
