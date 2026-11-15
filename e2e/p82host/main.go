// Command p82host is the Go host the Phase 8.2 e2e tests build and run
// (e2e/p82_*_test.go). It is not an example: every effect exists to test
// one guarantee of host effects through a real process.
//
//	p82host [-cap kind=scope]... [-log FILE] [-server URL -id ID]
//	        [-steps N] [-alloc-mb N] [-timeout-ms N] [-setup NAME]
//	        [-record FILE | -replay FILE | -resume FILE] SCRIPT
//
// Kinds: k8s (exact match, declared by its effects) and team (declared
// with subtree coverage: team("a") covers team("a/b")). Every Fn appends a
// line to -log before it does anything else, so a test can prove that a
// denied call never reached Fn. Exit codes: 0 success, 1 an error from
// arv.Run (stderr "error: MSG" then "kind: KIND"), 2 bad usage, 3 a
// goroutine outlived arv.Run.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// secret stands for an API key the host holds. Effects use it; no Arv
// value ever carries it.
const secret = "sk-p82-host-secret-0123456789"

type host struct {
	logMu  sync.Mutex
	log    io.Writer
	server string
	id     string
	client *http.Client
}

// note appends one line to the call log.
func (h *host) note(format string, a ...any) {
	h.logMu.Lock()
	defer h.logMu.Unlock()
	fmt.Fprintf(h.log, format+"\n", a...)
}

// get calls the e2e server (a gate route) and returns the body.
func (h *host) get(ctx context.Context, path string, q url.Values) (string, error) {
	if h.server == "" {
		return "", errors.New("no -server")
	}
	q.Set("id", h.id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.server+path+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// first is every effect's Scope: its first argument, a string.
func first(args []value.Object) (string, error) {
	if len(args) == 0 {
		return "", errors.New("want a scope as the first argument")
	}
	s, ok := args[0].(*value.String)
	if !ok {
		return "", fmt.Errorf("the scope must be STRING, got %s", args[0].Type())
	}
	return s.Value, nil
}

func str(s string) value.Object { return &value.String{Value: s} }

// fn wraps an effect body so every call is logged first, as name(scope).
func (h *host) fn(name string, body func(t *runtime.Task, args []value.Object) (value.Object, error)) func(*runtime.Task, ...value.Object) (value.Object, error) {
	return func(t *runtime.Task, args ...value.Object) (value.Object, error) {
		scope, _ := first(args)
		h.note("%s(%s) task=%s", name, scope, t.ID)
		return body(t, args)
	}
}

func (h *host) effects() map[string]arv.Effect {
	k8s := func(name string, body func(*runtime.Task, []value.Object) (value.Object, error)) arv.Effect {
		return arv.Effect{Kind: "k8s", Scope: first, Fn: h.fn(name, body)}
	}
	return map[string]arv.Effect{
		"k8s_apply": k8s("k8s_apply", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			if len(a) != 2 {
				return nil, fmt.Errorf("want (namespace, manifest), got %d arguments", len(a))
			}
			return value.NewHash(map[string]value.Object{"namespace": a[0], "applied": a[1]}), nil
		}),
		"team_read": {Kind: "team", Scope: first, Fn: h.fn("team_read", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			return str("read " + a[0].(*value.String).Value), nil
		})},
		// k8s_wait arrives at gate "fn" on the server, then waits for its
		// task to be cancelled and logs that it saw it.
		"k8s_wait": k8s("k8s_wait", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			// The arrival may open a gate that leads to the cancel before
			// the response is read: a cancel seen there counts too.
			if _, err := h.get(t.Ctx, "/arrive", url.Values{"k": {"fn"}}); err != nil && t.Ctx.Err() == nil {
				return nil, err
			}
			<-t.Ctx.Done()
			h.note("k8s_wait cancelled task=%s", t.ID)
			return nil, context.Cause(t.Ctx)
		}),
		// k8s_gate waits at the server's gate a[1] (/wait, one arrival)
		// and returns its answer.
		"k8s_gate": k8s("k8s_gate", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			if len(a) != 2 {
				return nil, errors.New("want (namespace, gate)")
			}
			gate, ok := a[1].(*value.String)
			if !ok {
				return nil, errors.New("the gate must be STRING")
			}
			body, err := h.get(t.Ctx, "/wait", url.Values{"k": {gate.Value}, "n": {"1"}})
			if err != nil {
				return nil, err
			}
			return str(body), nil
		}),
		// k8s_call calls the server with the host's secret and returns
		// only whether it answered.
		"k8s_call": k8s("k8s_call", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			body, err := h.get(t.Ctx, "/ok", url.Values{})
			if err != nil {
				return nil, fmt.Errorf("server unreachable (%d bytes)", len(body))
			}
			return str("answered " + strconv.Itoa(len(body)) + " bytes"), nil
		}),
		"k8s_fail": k8s("k8s_fail", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			msg := "failed"
			if len(a) > 1 {
				if s, ok := a[1].(*value.String); ok {
					msg = s.Value
				}
			}
			return nil, errors.New(msg)
		}),
		"k8s_deny": k8s("k8s_deny", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			return nil, &runtime.DeniedError{Task: t.ID, Want: runtime.Capability{Kind: "k8s", Scope: "upstream"}, Note: "the cluster refused"}
		}),
		"k8s_panic": k8s("k8s_panic", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			panic("host bug in k8s_panic")
		}),
		// k8s_big returns a string of a[1] bytes.
		"k8s_big": k8s("k8s_big", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			n, ok := a[1].(*value.Integer)
			if !ok || n.Value < 0 || n.Value > 64<<20 {
				return nil, errors.New("want a size from 0 to 64 MiB")
			}
			return str(strings.Repeat("x", int(n.Value))), nil
		}),
		"k8s_echo": k8s("k8s_echo", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			return a[1], nil
		}),
		"k8s_func": k8s("k8s_func", func(t *runtime.Task, a []value.Object) (value.Object, error) {
			return &value.Builtin{Fn: func(*runtime.Task, ...value.Object) value.Object { return str(secret) }}, nil
		}),
	}
}

// subtree is team's coverage: reflexive and transitive.
func subtree(g, r string) bool { return r == g || strings.HasPrefix(r, g+"/") }

// options builds the run's options; setup names a deliberately bad host
// setup, which arv.Run must refuse before the program runs.
func (h *host) options(setup string) (arv.Options, error) {
	opts := arv.Options{
		Kinds:   []runtime.KindSpec{{Kind: "team", Covers: subtree}},
		Effects: h.effects(),
	}
	ok := opts.Effects["k8s_apply"]
	bad := func(name string, e arv.Effect) { opts.Effects[name] = e }
	switch setup {
	case "":
	case "nokind":
		bad("k8s_raw", arv.Effect{Scope: ok.Scope, Fn: ok.Fn})
	case "noscope":
		bad("k8s_raw", arv.Effect{Kind: "k8s", Fn: ok.Fn})
	case "nofn":
		bad("k8s_raw", arv.Effect{Kind: "k8s", Scope: ok.Scope})
	case "netkind":
		bad("k8s_raw", arv.Effect{Kind: runtime.Net, Scope: ok.Scope, Fn: ok.Fn})
	case "envkind":
		bad("k8s_raw", arv.Effect{Kind: runtime.Env, Scope: ok.Scope, Fn: ok.Fn})
	case "hidebuiltin":
		bad("puts", ok)
	case "keyword":
		bad("spawn", ok)
	case "ctorname":
		bad("team", ok)
	case "badname":
		bad("k8s-raw", ok)
	case "badkind":
		bad("k8s_raw", arv.Effect{Kind: "K8S", Scope: ok.Scope, Fn: ok.Fn})
	case "dupkind":
		opts.Kinds = append(opts.Kinds, runtime.KindSpec{Kind: "team"})
	case "builtinspec":
		opts.Kinds = append(opts.Kinds, runtime.KindSpec{Kind: runtime.FSRead})
	default:
		return opts, fmt.Errorf("unknown -setup %q", setup)
	}
	return opts, nil
}

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("p82host", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var caps list
	fs.Var(&caps, "cap", "grant `KIND=SCOPE` to the root; repeat for more")
	logPath := fs.String("log", "", "append each Fn call to `FILE`")
	h := &host{log: io.Discard}
	fs.StringVar(&h.server, "server", "", "the e2e server's `URL`")
	fs.StringVar(&h.id, "id", "", "the test's id for the server's gates")
	steps := fs.Int64("steps", 0, "steps budget")
	allocMB := fs.Int64("alloc-mb", 0, "alloc_mb budget")
	timeoutMS := fs.Int64("timeout-ms", 60000, "run timeout")
	setup := fs.String("setup", "", "a bad host setup to try")
	record := fs.String("record", "", "record the run's effect log to `FILE`")
	replay := fs.String("replay", "", "replay the effect log in `FILE`")
	resume := fs.String("resume", "", "resume the effect log in `FILE`")
	if err := fs.Parse(args); err != nil || fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: p82host [flags] SCRIPT")
		return 2
	}
	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	if *logPath != "" {
		f, err := os.OpenFile(*logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			fmt.Fprintln(stderr, "error:", err)
			return 2
		}
		defer f.Close()
		h.log = f
	}
	tr := &http.Transport{DisableKeepAlives: true}
	h.client = &http.Client{Transport: tr}
	opts, err := h.options(*setup)
	if err != nil {
		fmt.Fprintln(stderr, "usage:", err)
		return 2
	}
	for _, c := range caps {
		kind, scope, _ := strings.Cut(c, "=")
		opts.Caps = append(opts.Caps, runtime.Capability{Kind: runtime.Kind(kind), Scope: scope})
	}
	opts.Out = stdout
	opts.Limits = runtime.Limits{Steps: *steps, AllocBytes: *allocMB << 20, Timeout: time.Duration(*timeoutMS) * time.Millisecond}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	closeLog, err := logOptions(&opts, set, *record, *replay, *resume)
	if err != nil {
		fmt.Fprintln(stderr, "usage:", err)
		return 2
	}
	defer closeLog()

	before := goruntime.NumGoroutine()
	_, runErr := arv.Run(string(src), opts)
	tr.CloseIdleConnections()
	if code := leaked(before, stderr); code != 0 {
		return code
	}
	if runErr != nil {
		fmt.Fprintln(stderr, "error:", runErr)
		var ae *arv.Error
		if errors.As(runErr, &ae) {
			fmt.Fprintln(stderr, "kind:", ae.Kind)
		}
		return 1
	}
	return 0
}

// leaked waits (generously: the machine may be loaded) for the goroutine
// count to fall back to what it was before arv.Run, and reports 3 with
// every stack if it does not.
func leaked(before int, stderr io.Writer) int {
	deadline := time.Now().Add(20 * time.Second)
	for goruntime.NumGoroutine() > before {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<20)
			fmt.Fprintf(stderr, "error: leak: %d goroutines outlived arv.Run (%d before)\n%s\n",
				goruntime.NumGoroutine(), before, buf[:goruntime.Stack(buf, true)])
			return 3
		}
		time.Sleep(5 * time.Millisecond)
	}
	return 0
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }
