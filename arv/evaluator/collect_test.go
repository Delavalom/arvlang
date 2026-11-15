package evaluator

import (
	"fmt"
	"math/rand"
	goruntime "runtime"
	"strings"
	"testing"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

const collectTasks = `let ok = fn(n) { wait(); n };
	let bad = fn() { getenv("ARV_COLLECT_X") };
`

// Acceptance: collect. The middle task fails; its siblings are not
// cancelled (they finish only after the failure has been seen), and every
// outcome is there to match.
func TestCollect(t *testing.T) {
	got := runBounded(t, 10*time.Second, collectTasks+`
		scope (supervise: "collect") {
			let ts = [spawn ok(1), spawn bad(), spawn ok(3)];
			let seen = match await ts[1] { ok(v) => v, err(e) => e["kind"] };
			open();
			for t in ts { match await t { ok(v) => v, err(e) => e["kind"] } }
		}`, gate())
	if got.Inspect() != "[1, capability, 3]" {
		t.Fatalf("got %s, want [1, capability, 3]", got.Inspect())
	}
}

// Acceptance: collect never drops. A failure nobody awaited fails the
// scope when it closes, with the count, and the first one's message,
// kind, position and task.
func TestCollectNeverDrops(t *testing.T) {
	got := runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") { spawn bad(); spawn bad(); open(); 1 }`, gate())
	e := wantKind(t, got, runtime.KindCapability)
	if !strings.HasPrefix(e.Message, "2 unawaited task failures: capability denied: env(\"ARV_COLLECT_X\") in task root/0") ||
		e.Task != "root/0" || e.Pos != "2:19" { // where getenv is, in bad
		t.Fatalf("got %q at %s in %s", e.Message, e.Pos, e.Task)
	}
	got = runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") { spawn bad(); 1 }`, gate())
	wantErrorHas(t, got, "1 unawaited task failures: ")
	// Awaited (here through match), a failure is not reported again.
	got = runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") {
		let t = spawn bad(); match await t { ok(v) => v, err(e) => "seen" } }`, gate())
	if got.Inspect() != "seen" {
		t.Fatalf("got %s, want seen", got.Inspect())
	}
	// A task that succeeded need not be awaited.
	got = runBounded(t, 10*time.Second, collectTasks+`scope (supervise: "collect") { spawn ok(1); open(); 2 }`, gate())
	if got.Inspect() != "2" {
		t.Fatalf("got %s, want 2", got.Inspect())
	}
}

// Acceptance: escalation. An inner restart scope that runs out of retries
// fails its task; the outer collect scope sees one failed task, and the
// others finish.
func TestEscalation(t *testing.T) {
	got, _ := runWith(t, `let flaky = fn() { error("down") };
		let inner = fn() { scope (supervise: "restart", retries: 2, backoff_ms: 1, retry_on: ["user"]) { await spawn flaky() } };
		let n = fn(x) { x };
		scope (supervise: "collect") {
			let a = spawn inner(); let b = spawn n(2); let c = spawn n(3);
			for t in [a, b, c] { match await t { ok(v) => v, err(e) => [e["kind"], e["task"]] } }
		}`)
	if got.Inspect() != "[[user, root/0/0#3], 2, 3]" {
		t.Fatalf("got %s", got.Inspect())
	}
}

// The body's own error still fails a collect scope and cancels its tasks.
func TestCollectBodyErrorFailsFast(t *testing.T) {
	start := time.Now()
	got, _ := runWith(t, `let slow = fn() { sleep(10000) }; scope (supervise: "collect") { spawn slow(); 1 / 0 }`)
	wantErrorHas(t, got, "division by zero")
	if time.Since(start) > 5*time.Second {
		t.Fatal("the body's error did not cancel the collect scope's tasks")
	}
}

// Under fail_fast, match cannot rescue a task's failure: the scope is
// already cancelled when the awaiter sees it, and fails with it (I5).
func TestFailFastMatchDoesNotRescue(t *testing.T) {
	for i := 0; i < 20; i++ {
		got, out := runWith(t, `let bad = fn() { getenv("ARV_COLLECT_X") };
			scope { let t = spawn bad(); match await t { ok(v) => v, err(e) => puts("caught") } }`)
		wantKind(t, got, runtime.KindCapability)
		if out != "" {
			t.Fatalf("run %d: the err arm ran in a failed fail_fast scope", i)
		}
	}
}

// Property: a collect scope never drops an error. For random tasks, some
// failing, some awaited, the scope fails exactly when a failed task was
// not awaited, and counts those.
func TestCollectNeverDropsProperty(t *testing.T) {
	rng := rand.New(rand.NewSource(4))
	for i := 0; i < 150; i++ {
		n := 1 + rng.Intn(5)
		var spawns, awaits []string
		dropped := 0
		for j := 0; j < n; j++ {
			fails, awaited := rng.Intn(2) == 0, rng.Intn(2) == 0
			f := "good"
			if fails {
				f = "bad"
			}
			spawns = append(spawns, fmt.Sprintf("let t%d = spawn %s();", j, f))
			if awaited {
				awaits = append(awaits, fmt.Sprintf("match await t%d { ok(v) => v, err(e) => 0 };", j))
			} else if fails {
				dropped++
			}
		}
		src := `let good = fn() { 1 }; let bad = fn() { error("lost") };
			scope (supervise: "collect") { ` + strings.Join(spawns, " ") + strings.Join(awaits, " ") + ` "done" }`
		got, _ := runWith(t, src)
		if dropped == 0 && got.Inspect() != "done" {
			t.Fatalf("%s\n= %s, want done", src, got.Inspect())
		}
		if dropped > 0 {
			if e := wantKind(t, got, runtime.KindUser); e.Message != fmt.Sprintf("%d unawaited task failures: lost", dropped) {
				t.Fatalf("%s\n= %q, want %d unawaited", src, e.Message, dropped)
			}
		}
	}
}

// Collected failures leave no goroutine behind.
func TestCollectLeaksNothing(t *testing.T) {
	before := goruntime.NumGoroutine()
	got, _ := runWith(t, `let bad = fn(n) { error("x") }; let slow = fn() { sleep(20) };
		match scope (supervise: "collect") { for n in [1, 2, 3, 4, 5, 6] { spawn bad(n) }; spawn slow(); 1 } { ok(v) => v, err(e) => e["message"] }`)
	if got.Inspect() != "6 unawaited task failures: x" {
		t.Fatalf("got %s", got.Inspect())
	}
	settled(t, before)
}
