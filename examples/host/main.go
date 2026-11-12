// Command host is an example Go host for Arv (Phase 8.2). It adds one
// effect, k8s_apply(namespace, manifest), backed by a fake in-memory
// cluster. Every call needs a k8s(namespace) grant, which only the host
// can give (-allow), and a task can hand on to a child it spawns.
//
//	go run ./examples/host                  # denied: the run holds no k8s grant
//	go run ./examples/host -allow staging   # applies to staging; prod stays denied
//	go run ./examples/host -allow staging -record run.jsonl
//	go run ./examples/host -replay run.jsonl  # same output; the cluster is untouched
//
// A replay calls no effect: its answers come from the log, so the fake
// cluster it prints at the end is empty.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"maps"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// script deploys to staging from the root and from a child holding only
// k8s("staging"), then tries prod and reports what kind of error it got.
const script = `
let deploy = fn(ns, manifest) { k8s_apply(ns, manifest)["applied"] };
puts(deploy("staging", "web.yaml"));
let t = spawn deploy("staging", "api.yaml") with [k8s("staging")];
puts(await t);
match k8s_apply("prod", "web.yaml") {
  ok(v) => puts("prod: applied"),
  err(e) => puts("prod: " + e["kind"])
}
`

// cluster is the fake: what each namespace has had applied, in order.
type cluster struct {
	mu      sync.Mutex
	applied map[string][]string
}

// args reads k8s_apply's (namespace, manifest).
func args(a []value.Object) (ns, manifest string, err error) {
	if len(a) != 2 {
		return "", "", fmt.Errorf("want 2 arguments (namespace, manifest), got %d", len(a))
	}
	n, ok1 := a[0].(*value.String)
	m, ok2 := a[1].(*value.String)
	if !ok1 || !ok2 {
		return "", "", errors.New("namespace and manifest must be STRING")
	}
	return n.Value, m.Value, nil
}

// namespace is k8s_apply's Scope: the call needs k8s(namespace).
func namespace(a []value.Object) (string, error) {
	ns, _, err := args(a)
	return ns, err
}

// apply is k8s_apply's Fn. The runtime calls it only once the calling
// task holds k8s(namespace); a real host would talk to the API server
// here, honouring t.Ctx.
func (c *cluster) apply(t *runtime.Task, a ...value.Object) (value.Object, error) {
	ns, manifest, err := args(a)
	if err != nil {
		return nil, err
	}
	if t.Ctx.Err() != nil {
		return nil, context.Cause(t.Ctx)
	}
	c.mu.Lock()
	c.applied[ns] = append(c.applied[ns], manifest)
	c.mu.Unlock()
	return value.NewHash(map[string]value.Object{
		"namespace": &value.String{Value: ns},
		"applied":   &value.String{Value: manifest},
	}), nil
}

func (c *cluster) String() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var parts []string
	for _, ns := range slices.Sorted(maps.Keys(c.applied)) {
		parts = append(parts, fmt.Sprintf("%s=%v", ns, c.applied[ns]))
	}
	return strings.Join(parts, " ")
}

// logs names an effect log to write (record) or to replay (replay).
type logs struct{ record, replay string }

// run runs the script holding k8s(ns) for each namespace in allow, and
// returns the exit code. With lg.record it writes the run's effect log;
// with lg.replay it replays one, taking its grants and limits from it.
func run(allow []string, lg logs, stdout, stderr io.Writer) int {
	c := &cluster{applied: map[string][]string{}}
	var caps []runtime.Capability
	for _, ns := range allow {
		caps = append(caps, runtime.Capability{Kind: "k8s", Scope: ns})
	}
	opts := arv.Options{
		Caps:   caps,
		Out:    stdout,
		Limits: runtime.Limits{Timeout: 30 * time.Second},
		Effects: map[string]arv.Effect{
			"k8s_apply": {Kind: "k8s", Scope: namespace, Fn: c.apply},
		},
	}
	if err := lg.setup(&opts); err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if f, ok := opts.Record.(*os.File); ok {
		defer f.Close()
	}
	_, err := arv.Run(script, opts)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintf(stdout, "cluster: %s\n", c)
	return 0
}

// setup opens lg's log for opts.
func (lg logs) setup(opts *arv.Options) error {
	switch {
	case lg.record != "" && lg.replay != "":
		return errors.New("-record or -replay, not both")
	case lg.record != "":
		f, err := os.OpenFile(lg.record, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return err
		}
		opts.Record = f
	case lg.replay != "":
		if len(opts.Caps) > 0 {
			return errors.New("a replay takes its grants from the log: drop -allow")
		}
		f, err := os.Open(lg.replay)
		if err != nil {
			return err
		}
		defer f.Close()
		l, err := runtime.ReadLog(f)
		if err != nil {
			return err
		}
		opts.Replay, opts.Limits = l, runtime.Limits{}
	}
	return nil
}

type list []string

func (l *list) String() string     { return strings.Join(*l, ",") }
func (l *list) Set(s string) error { *l = append(*l, s); return nil }

func main() {
	var allow list
	var lg logs
	flag.Var(&allow, "allow", "grant k8s(`NAMESPACE`); repeat for more")
	flag.StringVar(&lg.record, "record", "", "write the run's effect log to `FILE`")
	flag.StringVar(&lg.replay, "replay", "", "replay the effect log in `FILE`")
	flag.Parse()
	os.Exit(run(allow, lg, os.Stdout, os.Stderr))
}
