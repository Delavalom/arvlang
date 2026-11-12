# Phase 7, task p7.3: denial redaction

> **For agentic workers:** this file is one task, p7.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: task p7.2 is committed in your tree. Start every shell command with `cd <your worktree> &&` and use absolute paths: the shell's working directory does not persist. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify` and never push; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2): `t.Chdir`, `min` and `max` are available; do not use `testing/synctest` or `sync.WaitGroup.Go`. Before a long test run, check `df -h /System/Volumes/Data`; below 1 GB free, stop and report (only `go clean -cache` may be deleted). This machine is heavily loaded: keep wall-clock bounds generous. No test may reach the network or spend money: never set `ARV_LIVE_ANTHROPIC`, and never put a real API key anywhere.
>
> Every code block below was applied, in task order, to a scratch copy of `feat/phases-3-8` (Phases 3, 4 and 5 merged; Phase 6 is not) and passed `gofmt -l .`, `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `14-phase7-agents.md`. Deviations from it, and why: `p7-plan-1.md`.

### Task p7.3: denial redaction

A `DeniedError`'s message lists up to 8 grants the task holds, and a dial-time one names the internal address a granted name resolved to. None of that may reach a model. `(*DeniedError).Redacted()` is the denial as a model may see it: `capability denied: ` plus what was asked for, and for `net` only `capability denied: net` (the model knows what it asked for; naming the host for one kind of denial and not the other would tell it which names resolve inside). `RedactDenials(msg)` works on text, because Arv programs pass error messages on as strings: it keeps what comes before the first `capability denied: `, and replaces that denial and everything after it with the redacted form (just `capability denied` when the request cannot be parsed back). Where a quoted held list ends cannot be told reliably, so nothing after it is kept.

The fuzz target checks the property on any input; run it for a while once (`go test -run '^$' -fuzz FuzzRedactDenials -fuzztime 30s ./arv/runtime/`) but do not commit the corpus it writes under `testdata/`.

**Files:**
- Create: `arv/runtime/redact.go`
- Create (test): `arv/runtime/redact_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/redact_test.go`:

```go
package runtime

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestRedactDenials(t *testing.T) {
	held := []Capability{{Net, "secret.internal"}, {FSRead, "/home/me/private"}, {LLM, "anthropic:*"}}
	cases := []struct {
		err  *DeniedError
		want string
	}{
		{&DeniedError{Task: "root/0", Want: Capability{Tool, "notes"}, Held: held}, `capability denied: tool("notes")`},
		{&DeniedError{Task: "root", Want: Capability{FSRead, "/etc/passwd"}, Held: held}, `capability denied: fs_read("/etc/passwd")`},
		{&DeniedError{Want: Capability{Env, "HOME"}, Held: held}, `capability denied: env("HOME")`},
		{&DeniedError{Task: "root/1", Want: Capability{Net, "evil.example:80"}, Held: held}, `capability denied: net`},
		{&DeniedError{Task: "root/1", Want: Capability{Net, "10.0.0.5:80"}, Held: held,
			Note: "metadata.example:80 reached a private address, which only an IP or CIDR grant covers"}, `capability denied: net`},
	}
	for _, c := range cases {
		for _, prefix := range []string{"", "http_get: ", "spawn in task root: "} {
			got := RedactDenials(prefix + c.err.Error())
			if got != prefix+c.want || got != prefix+c.err.Redacted() {
				t.Errorf("RedactDenials(%q) = %q, want %q", prefix+c.err.Error(), got, prefix+c.want)
			}
			for _, leak := range []string{"secret.internal", "/home/me/private", "anthropic:*", "holds", "root/", "10.0.0.5", "private address"} {
				if strings.Contains(got, leak) {
					t.Errorf("RedactDenials(%q) = %q leaks %q", prefix+c.err.Error(), got, leak)
				}
			}
		}
	}
	for in, want := range map[string]string{
		"no denial here":                   "no denial here",
		"capability denied: bogus(\"x\")":  "capability denied",
		"capability denied: net(\"x":       "capability denied",
		"x capability denied: tool(\"a\"]": "x capability denied",
	} {
		if got := RedactDenials(in); got != want {
			t.Errorf("RedactDenials(%q) = %q, want %q", in, got, want)
		}
	}
}

// Random denials, with scopes that contain quotes, brackets and the
// prefix itself: the redacted text never names a held grant.
func TestRedactDenialsRandom(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	pieces := []string{"a", `"`, "]", ")", " (holds: [", deniedPrefix, `\`, "\n", "é"}
	word := func() string {
		var b strings.Builder
		for n := r.Intn(6); n >= 0; n-- {
			b.WriteString(pieces[r.Intn(len(pieces))])
		}
		return b.String()
	}
	for i := 0; i < 3000; i++ {
		kinds := []Kind{Net, FSRead, FSWrite, Env, LLM, Tool}
		want := Capability{kinds[r.Intn(len(kinds))], word()}
		secret := fmt.Sprintf("SECRET%d", i)
		e := &DeniedError{Task: "root/" + secret, Want: want, Held: []Capability{{Env, secret}}}
		if r.Intn(2) == 0 {
			e.Note = secret + " reached a loopback address"
		}
		got := RedactDenials(e.Error())
		if got != e.Redacted() || strings.Contains(got, "SECRET") {
			t.Fatalf("RedactDenials(%q) = %q, want %q", e.Error(), got, e.Redacted())
		}
	}
}

func FuzzRedactDenials(f *testing.F) {
	f.Add("capability denied: net(\"a:80\") in task root (holds: [])")
	f.Add("x: capability denied: tool(\"t\") (holds: [tool(\"u\")])")
	f.Fuzz(func(t *testing.T, msg string) {
		got := RedactDenials(msg)
		i := strings.Index(msg, deniedPrefix)
		if i < 0 {
			if got != msg {
				t.Fatalf("no denial, but %q became %q", msg, got)
			}
			return
		}
		tail, ok := strings.CutPrefix(got, msg[:i])
		if !ok {
			t.Fatalf("RedactDenials(%q) = %q: the text before the denial changed", msg, got)
		}
		// The tail is a bare denial, or names exactly one request.
		if tail == "capability denied" || tail == deniedPrefix+"net" {
			return
		}
		if again := RedactDenials(tail); again != tail || !strings.HasSuffix(tail, ")") {
			t.Fatalf("RedactDenials(%q) = %q, not a redacted denial", msg, got)
		}
	})
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test -run '^(TestRedactDenials|TestRedactDenialsRandom|FuzzRedactDenials)$' ./arv/runtime/`

Expected: FAIL: the package does not compile. The output includes:

```text
# github.com/delavalom/arvlang/arv/runtime [github.com/delavalom/arvlang/arv/runtime.test]
arv/runtime/redact_test.go:25:11: undefined: RedactDenials
arv/runtime/redact_test.go:26:51: c.err.Redacted undefined (type *DeniedError has no field or method Redacted)
arv/runtime/redact_test.go:42:13: undefined: RedactDenials
```

- [ ] **Step 3: Implement**

Create `arv/runtime/redact.go`:

```go
package runtime

import (
	"strconv"
	"strings"
)

// deniedPrefix starts every *DeniedError's message.
const deniedPrefix = "capability denied: "

// Redacted is the denial as a model may see it: what was asked for, and
// nothing about the task's other grants, its ID or a dial-time note. A
// net denial names no host or address at all: a dial-time denial's Want
// is the internal address a granted name resolved to, and naming the host
// only for the other kind would tell the model which names resolve
// inside. The model knows what it asked for.
func (e *DeniedError) Redacted() string {
	if e.Want.Kind == Net {
		return deniedPrefix + string(Net)
	}
	return deniedPrefix + e.Want.String()
}

// RedactDenials returns msg with its first capability denial, and
// everything after it, replaced by the denial's redacted form (see
// DeniedError.Redacted). A denial lists the grants the task holds (up to
// 8, maxHeldShown), and a dial-time one names the internal address a
// granted name resolved to; neither may reach a model (Phase 7). The text
// before the denial is kept. Everything after it is dropped: a quoted
// scope can hold any text, so where a held list ends cannot be told
// reliably, and a second denial there would carry its own list.
//
// It works on text, so it finds denials an Arv program passed on as
// strings (a tool that returns e["message"] from a match), not ones it
// disguised. If the request cannot be parsed back, the redacted form is
// just "capability denied".
func RedactDenials(msg string) string {
	i := strings.Index(msg, deniedPrefix)
	if i < 0 {
		return msg
	}
	keep := msg[:i]
	rest := msg[i+len(deniedPrefix):]
	open := strings.IndexByte(rest, '(')
	if open < 0 || !kinds[Kind(rest[:open])] {
		return keep + "capability denied"
	}
	kind := Kind(rest[:open])
	q, err := strconv.QuotedPrefix(rest[open+1:])
	if err != nil || !strings.HasPrefix(rest[open+1+len(q):], ")") {
		return keep + "capability denied"
	}
	scope, err := strconv.Unquote(q)
	if err != nil {
		return keep + "capability denied"
	}
	return keep + (&DeniedError{Want: Capability{Kind: kind, Scope: scope}}).Redacted()
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race -run '^(TestRedactDenials|TestRedactDenialsRandom|FuzzRedactDenials)$' ./arv/runtime/`, then `gofmt -l .`, `go vet ./...` and `go test -race ./...`.
Expected: PASS everywhere.

- [ ] **Step 5: Commit**

```bash
git add arv/runtime/redact_test.go arv/runtime/redact.go
git commit -m "feat(runtime): redact capability denials for models

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>"
```
