package e2e

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Phase 6 properties through the binary (I7). Random programs: trees of
// spawned tasks that print, sleep, fetch, read and write files, read the
// environment and draw random numbers.
//
//   - record, then replay: byte-identical output and exit status, no
//     request, no file touched, the recorded environment;
//   - record, SIGKILL, resume: the output an uninterrupted run prints,
//     each request made once (or, killed anywhere, at most once more).

// p6Grants are the root's grants in every generated program; children
// get all of them.
var p6Grants = []string{"--allow", "net=127.0.0.1", "--allow", "fs_read=d", "--allow", "fs_write=d", "--allow", "env=ARV_P6_E"}

const p6Caps = `[net("127.0.0.1"), fs_read("d"), fs_write("d"), env("ARV_P6_E")]`

// p6Gen writes one program. With seq, every spawned task is awaited at
// once and nothing is random, so two runs print the same lines in the
// same order; with gate, one request waits at /wait?k={{K}}.
type p6Gen struct {
	r          *rand.Rand
	seq, gate  bool
	fns        []string
	n, gets    int // tasks and /ok requests so far
	gatePlaced bool
	failed     bool
}

func (g *p6Gen) op(b *strings.Builder, name string, i, depth int) {
	switch k := g.r.Intn(11); {
	case k == 0:
		fmt.Fprintf(b, "puts(%q);\n", fmt.Sprintf("%s.%d", name, i))
	case k == 1:
		fmt.Fprintf(b, "sleep(%d);\n", 1+g.r.Intn(15))
	case k == 2 || k == 3:
		g.gets++
		fmt.Fprintf(b, "puts(http_get(\"{{SERVER}}/ok?id={{ID}}&n=%d\")[\"body\"]);\n", g.gets)
	case k == 4:
		f := fmt.Sprintf("d/%s_%d", name, i)
		fmt.Fprintf(b, "write_file(%q, %q + getenv(\"ARV_P6_E\")); puts(read_file(%q));\n", f, name, f)
	case k == 5 && !g.seq:
		fmt.Fprintf(b, "puts([random(1000000), now() > 0]);\n")
	case k == 6 && g.gate && !g.gatePlaced && g.r.Intn(2) == 0:
		g.gatePlaced = true
		fmt.Fprintf(b, "puts(http_get(\"{{SERVER}}/wait?id={{ID}}&k={{K}}&n=1\")[\"body\"]);\n")
	case k <= 8 && depth < 3:
		child := g.task(depth + 1)
		fmt.Fprintf(b, "let h%d = spawn %s() with %s;\n", i, child, p6Caps)
		if g.seq || g.r.Intn(3) > 0 {
			fmt.Fprintf(b, "puts(await h%d);\n", i)
		}
	case k == 9 && depth < 3 && !g.seq:
		a, c := g.task(depth+1), g.task(depth+1)
		fmt.Fprintf(b, "puts(match scope { let x = spawn %s() with %s; let y = spawn %s() with %s; [await x, await y] } { ok(v) => v, err(e) => e[\"message\"] });\n", a, p6Caps, c, p6Caps)
	case k == 10 && !g.failed && g.r.Intn(4) == 0:
		g.failed = true
		fmt.Fprintf(b, "error(%q);\n", "boom in "+name)
	}
}

func (g *p6Gen) task(depth int) string {
	g.n++
	name := fmt.Sprintf("t%d", g.n)
	var b strings.Builder
	for i, n := 0, 1+g.r.Intn(5); i < n; i++ {
		g.op(&b, name, i, depth)
	}
	g.fns = append(g.fns, fmt.Sprintf("let %s = fn() {\n%s%q\n};\n", name, b.String(), name+" done"))
	return name
}

func (g *p6Gen) program() string {
	top := g.task(0)
	last := ""
	if g.gate && !g.gatePlaced {
		last = "puts(http_get(\"{{SERVER}}/wait?id={{ID}}&k={{K}}&n=1\")[\"body\"]);\n"
	}
	return strings.Join(g.fns, "") + last + fmt.Sprintf("puts(await spawn %s() with %s)\n", top, p6Caps)
}

// p6Dir is a fresh working directory with the program at prog.arv and an
// empty d/, and the program's path.
func p6Dir(t *testing.T, src string, k string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "prog.arv")
	mustWrite(t, script, expand(t, src, map[string]string{"K": k}))
	if err := os.Mkdir(filepath.Join(dir, "d"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, script
}

// files reads every file in dir/d.
func files(t *testing.T, dir string) map[string]string {
	t.Helper()
	ents, err := os.ReadDir(filepath.Join(dir, "d"))
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]string{}
	for _, e := range ents {
		b, err := os.ReadFile(filepath.Join(dir, "d", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		m[e.Name()] = string(b)
	}
	return m
}

func p6Run(t *testing.T, dir string, env string, args ...string) result {
	t.Helper()
	res := run(t, spec{args: args, dir: dir, env: []string{"ARV_P6_E=" + env}, timeout: 2 * time.Minute})
	if strings.Contains(res.stderr, "panic") || strings.Contains(res.stderr, "goroutine ") {
		t.Fatalf("the binary panicked\n%s", res)
	}
	return res
}

func p6Cases(n int) int {
	if testing.Short() {
		return n / 4
	}
	return n
}

// I7 through the binary: a replay prints what the recording printed,
// byte for byte, with the same status and error, and runs nothing live.
func TestP6PropRecordReplay(t *testing.T) {
	t.Parallel()
	for i := range p6Cases(40) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			g := &p6Gen{r: rand.New(rand.NewSource(int64(i)))}
			src := g.program()
			dir, script := p6Dir(t, src, "")
			log := filepath.Join(dir, "run.jsonl")
			rec := p6Run(t, dir, "rec", append(append([]string{"run", "--record", log}, p6Grants...), script)...)
			if rec.code != 0 && !strings.Contains(rec.stderr, "boom in") {
				t.Fatalf("record\n%s\n%s", rec, src)
			}
			// A request a fail-fast cancelled in flight may still be on
			// its way to the handler: count it as the record's.
			drainServer(t)
			gets := hits(t, "/ok")
			if err := os.RemoveAll(filepath.Join(dir, "d")); err != nil {
				t.Fatal(err)
			}
			rep := p6Run(t, dir, "replay", "run", "--replay", log, script)
			head, rest, _ := strings.Cut(rep.stderr, "\n")
			if rep.code != rec.code || rep.stdout != rec.stdout || rest != rec.stderr || !strings.HasPrefix(head, "replay: grants: ") {
				t.Fatalf("replay differs\nrecord:\n%s\nreplay:\n%s\nprogram:\n%s", rec, rep, src)
			}
			if hits(t, "/ok") != gets {
				t.Fatalf("the replay made %d requests", hits(t, "/ok")-gets)
			}
			assertAbsent(t, filepath.Join(dir, "d"))
		})
	}
}

// The differential test: a run killed with SIGKILL while one request is
// held at a gate, then resumed, prints what an uninterrupted run prints,
// writes the same files, and makes each request once (the held one
// twice); a replay of the resumed log prints it again.
func TestP6PropKillAtGateResume(t *testing.T) {
	t.Parallel()
	for i := range p6Cases(20) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			// No failure: the gate must be reached.
			g := &p6Gen{r: rand.New(rand.NewSource(int64(1000 + i))), seq: true, gate: true, failed: true}
			src := g.program()
			arrive(testID(t), "open")
			refDir, refScript := p6Dir(t, src, "open")
			ref := p6Run(t, refDir, "e", append(append([]string{"run"}, p6Grants...), refScript)...)
			if ref.code != 0 || hits(t, "/ok") != g.gets || hits(t, "/wait") != 1 {
				t.Fatalf("uninterrupted run: %d of %d requests\n%s\n%s", hits(t, "/ok"), g.gets, ref, src)
			}

			dir, script := p6Dir(t, src, "shut")
			log := filepath.Join(dir, "run.jsonl")
			cmd, _ := startArv(t, dir, []string{"ARV_P6_E=e"}, append(append([]string{"run", "--record", log}, p6Grants...), script)...)
			waitHits(t, "/wait", 2)
			cmd.Process.Kill()
			cmd.Wait()
			drainServer(t)
			arrive(testID(t), "shut")
			res := p6Run(t, dir, "e", append(append([]string{"run", "--resume", log}, p6Grants...), script)...)
			drainServer(t)
			if res.code != ref.code || res.stdout != ref.stdout || res.stderr != ref.stderr {
				t.Fatalf("resume differs\nuninterrupted:\n%s\nresumed:\n%s\nprogram:\n%s", ref, res, src)
			}
			if hits(t, "/ok") != 2*g.gets || hits(t, "/wait") != 3 {
				t.Fatalf("requests: %d /ok (want %d), %d /wait (want 3)", hits(t, "/ok"), 2*g.gets, hits(t, "/wait"))
			}
			if a, b := fmt.Sprint(files(t, refDir)), fmt.Sprint(files(t, dir)); a != b {
				t.Fatalf("files differ: %s, want %s", b, a)
			}
			rep := p6Run(t, dir, "other", "run", "--replay", log, script)
			drainServer(t)
			if rep.code != ref.code || rep.stdout != ref.stdout || hits(t, "/ok") != 2*g.gets || hits(t, "/wait") != 3 {
				t.Fatalf("replay of the resumed log\n%s", rep)
			}
		})
	}
}

// The differential test, killed anywhere: SIGKILL at a random moment
// of the recording (a request in flight, a file half written, a log line
// half written, or after the end), then --resume, gives the
// uninterrupted run's output, status and files; the request in flight,
// if any, is made once more, no other.
func TestP6PropKillAnywhereResume(t *testing.T) {
	t.Parallel()
	var midRun atomic.Int32 // cases killed before the run's end
	t.Cleanup(func() {
		t.Logf("%d of %d cases were killed before the end", midRun.Load(), p6Cases(40))
		if !t.Failed() && midRun.Load() == 0 {
			t.Error("every case was killed after the run's end: the test resumed nothing")
		}
	})
	for i := range p6Cases(40) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			r := rand.New(rand.NewSource(int64(2000 + i)))
			g := &p6Gen{r: r, seq: true}
			src := g.program()
			refDir, refScript := p6Dir(t, src, "")
			ref := p6Run(t, refDir, "e", append(append([]string{"run"}, p6Grants...), refScript)...)
			if ref.code != 0 && !strings.Contains(ref.stderr, "boom in") {
				t.Fatalf("uninterrupted run\n%s\n%s", ref, src)
			}
			drainServer(t)
			refGets := hits(t, "/ok")

			dir, script := p6Dir(t, src, "")
			log := filepath.Join(dir, "run.jsonl")
			cmd, out := startArv(t, dir, []string{"ARV_P6_E=e"}, append(append([]string{"run", "--record", log}, p6Grants...), script)...)
			for deadline := time.Now().Add(time.Minute); ; time.Sleep(time.Millisecond) {
				if fi, err := os.Stat(log); err == nil && fi.Size() > 0 {
					break
				}
				if time.Now().After(deadline) {
					cmd.Process.Kill()
					cmd.Wait() // out is safe to read once Wait returns
					t.Fatalf("the recording wrote no header in a minute (%v); its output:\n%s", cmd.ProcessState, out)
				}
			}
			// Spread the kills over the program: after its j-th request
			// arrives, and a few milliseconds more.
			if refGets > 0 {
				waitHits(t, "/ok", refGets+1+r.Intn(refGets))
			}
			time.Sleep(time.Duration(r.Intn(4)) * time.Millisecond)
			cmd.Process.Kill()
			cmd.Wait()
			drainServer(t) // the request in flight at the kill, if any
			res := p6Run(t, dir, "e", append(append([]string{"run", "--resume", log}, p6Grants...), script)...)
			drainServer(t)
			stderr := strings.ReplaceAll(res.stderr, dir, refDir)
			if strings.HasPrefix(stderr, "replay: the run finished") { // killed after the end
				_, stderr, _ = strings.Cut(stderr, "\n")
			} else {
				midRun.Add(1)
			}
			if res.code != ref.code || res.stdout != ref.stdout || stderr != ref.stderr {
				t.Fatalf("resume differs\nuninterrupted:\n%s\nresumed:\n%s\nprogram:\n%s", ref, res, src)
			}
			if n := hits(t, "/ok") - refGets; n < refGets || n > refGets+1 {
				t.Fatalf("killed and resumed, the program made %d requests; uninterrupted, %d", n, refGets)
			}
			if a, b := fmt.Sprint(files(t, refDir)), fmt.Sprint(files(t, dir)); a != b {
				t.Fatalf("files differ: %s, want %s", b, a)
			}
		})
	}
}
