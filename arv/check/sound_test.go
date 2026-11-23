package check

import (
	"fmt"
	"math/rand"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv"
)

// gen builds random programs in which every leaf effect runs: each
// wrapper evaluates its hole exactly once, and a spawn hands its task
// exactly the capabilities of the leaves inside it. A leaf's argument is a
// literal, a + of the names the prelude binds, or a name a let binds
// inside a function, so folding (consts.go) is checked against what the
// runtime asks for. Net leaves reach a local server, by 127.0.0.1 and by
// 0.0.0.0 (which also needs a loopback grant when it dials).
type gen struct {
	r     *rand.Rand
	port  string
	leaf  int
	needs map[string]bool // the grants the leaves need, in --allow form
}

// prelude binds the names leaves fold.
func (g *gen) prelude() string {
	return `let lo = "http://127.0.0.1:` + g.port + `"; let un = "http://0.0.0.0:` + g.port + `"; ` +
		`let rp = "r"; let wp = "w"; let ext = ".txt"; let ep = "ARV_P81_"; `
}

// arg writes a call use(s) for the string s, known before running, in one
// of three forms: s as the literal lit, its folded form, or a name bound by
// a let next to the call (wrapped in a function, since a let is a
// statement). A constructor's text is repeated in the with list of every
// enclosing spawn, so it never takes the let form: the name would then be
// bound more than once, and not folded.
func (g *gen) arg(lit, folded string, use func(string) string, constructor bool) string {
	forms := 3
	if constructor {
		forms = 2
	}
	switch g.r.Intn(forms) {
	case 0:
		return use(`"` + lit + `"`)
	case 1:
		return use(folded)
	default:
		k := fmt.Sprintf("k%d", g.leaf)
		return "(fn() { let " + k + " = " + folded + "; " + use(k) + " })()"
	}
}

// leafExpr is one effect call, and its constructors for a spawn's with
// list.
func (g *gen) leafExpr() (call string, constructors []string) {
	g.leaf++
	n := g.r.Intn(4)
	call1 := func(fn string) func(string) string { return func(a string) string { return fn + "(" + a + ")" } }
	switch g.r.Intn(6) {
	case 0:
		lit, f := fmt.Sprintf("ARV_P81_%d", n), fmt.Sprintf(`ep + "%d"`, n)
		g.needs["env="+lit] = true
		return g.arg(lit, f, call1("getenv"), false), []string{g.arg(lit, f, call1("env"), true)}
	case 1:
		lit, f := fmt.Sprintf("r%d.txt", n), fmt.Sprintf(`rp + "%d" + ext`, n)
		g.needs["fs_read="+lit] = true
		return g.arg(lit, f, call1("read_file"), false), []string{g.arg(lit, f, call1("fs_read"), true)}
	case 2:
		lit, f := fmt.Sprintf("w%d.txt", n), fmt.Sprintf(`wp + "%d" + ext`, n)
		g.needs["fs_write="+lit] = true
		wr := func(a string) string { return "write_file(" + a + `, "x")` }
		return g.arg(lit, f, wr, false), []string{g.arg(lit, f, call1("fs_write"), true)}
	case 3:
		lo := "127.0.0.1:" + g.port
		g.needs["net="+lo] = true
		return g.arg("http://"+lo+"/", `lo + "/"`, call1("http_get"), false), []string{g.arg(lo, `"127.0.0.1:" + "`+g.port+`"`, call1("net"), true)}
	case 4: // http(req): the url key of a hash literal (Phase 10)
		lo := "127.0.0.1:" + g.port
		g.needs["net="+lo] = true
		req := func(a string) string { return `http({"method": "POST", "url": ` + a + `, "body": "b"})` }
		return g.arg("http://"+lo+"/", `lo + "/"`, req, false), []string{g.arg(lo, `"127.0.0.1:" + "`+g.port+`"`, call1("net"), true)}
	default:
		lo, un := "127.0.0.1:"+g.port, "0.0.0.0:"+g.port
		g.needs["net="+lo], g.needs["net="+un] = true, true
		return g.arg("http://"+un+"/", `un + "/"`, call1("http_get"), false),
			[]string{g.arg(un, `"0.0.0.0:" + "`+g.port+`"`, call1("net"), true), g.arg(lo, `"127.0.0.1:" + "`+g.port+`"`, call1("net"), true)}
	}
}

// expr returns an expression and the constructors of the leaves in it.
func (g *gen) expr(depth int) (string, []string) {
	if depth == 0 || g.r.Intn(4) == 0 {
		k := 1 + g.r.Intn(2)
		var calls, caps []string
		for i := 0; i < k; i++ {
			c, k := g.leafExpr()
			calls, caps = append(calls, c), append(caps, k...)
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
// meets a capability denial (soundness), the reported set is exactly what
// the leaves need, and leaving any one grant out is denied (every
// reported grant is used: these programs run every leaf).
func TestReportedGrantsSufficeAndAreNeeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) }))
	defer srv.Close()
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	dir := t.TempDir()
	t.Chdir(dir)
	for n := 0; n < 4; n++ {
		if err := os.WriteFile(fmt.Sprintf("r%d.txt", n), []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r := rand.New(rand.NewSource(81))
	for i := 0; i < 300; i++ {
		g := &gen{r: r, port: port, needs: map[string]bool{}}
		body, _ := g.expr(1 + r.Intn(4))
		src := g.prelude() + body + ";"
		checkGenerated(t, src, g.needs, dir)
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
