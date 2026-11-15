package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/parser"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// runLogged runs src in a root task holding caps, with the effect log l
// printing to out, then finishes l. It returns the run's result and
// Finish's error.
func runLogged(t *testing.T, src string, l *runtime.EffectLog, out *bytes.Buffer, caps ...runtime.Capability) (value.Object, error) {
	t.Helper()
	p := parser.New(lexer.New(src))
	program := p.ParseProgram()
	if len(p.Errors()) > 0 {
		t.Fatalf("parse: %v", p.Errors())
	}
	root := runtime.NewRoot(l.Start(context.Background()), runtime.NewCapSet(caps...), out)
	root.SetLog(l)
	res := New(root).Run(program, value.NewEnvironment())
	return res, l.Finish(value.EncodeRequest([]value.Object{res}))
}

// recordLogged records src; it returns what it printed, its result and
// its log.
func recordLogged(t *testing.T, src string, caps ...runtime.Capability) (string, value.Object, []byte) {
	t.Helper()
	var out, log bytes.Buffer
	l, err := runtime.NewRecorder(&log, runtime.Header{}, &out, nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := runLogged(t, src, l, &out, caps...)
	if err != nil {
		t.Fatalf("recording: %v", err)
	}
	return out.String(), res, log.Bytes()
}

// replayLogged replays log for src with the root holding caps.
func replayLogged(t *testing.T, src string, log []byte, caps ...runtime.Capability) (string, value.Object, error) {
	t.Helper()
	read, err := runtime.ReadLog(bytes.NewReader(log))
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	l, err := runtime.NewReplayer(read, &out)
	if err != nil {
		t.Fatal(err)
	}
	res, err := runLogged(t, src, l, &out, caps...)
	return out.String(), res, err
}

// I7 in the evaluator: every effect goes through the log, and a replay
// answers each from it: the environment, the clock and the random numbers
// of the recorded run, and its output, whatever the world says now.
func TestEffectsAreReplayed(t *testing.T) {
	t.Setenv("ARV_P6_GATE", "recorded")
	env, _ := runtime.New(runtime.Env, "ARV_P6_GATE")
	src := `puts(getenv("ARV_P6_GATE")); puts(random(1000000000)); let t = now(); sleep(1);
puts(clock_ms() > -1); [t, getenv("ARV_P6_GATE"), {"b": 1, "a": 2}]`
	printed, res, log := recordLogged(t, src, env)
	if !strings.HasPrefix(printed, "recorded\n") || isError(res) {
		t.Fatalf("recording printed %q, returned %s", printed, res.Inspect())
	}
	t.Setenv("ARV_P6_GATE", "changed")
	replayed, rres, err := replayLogged(t, src, log, env)
	if err != nil || replayed != printed || rres.Inspect() != res.Inspect() {
		t.Fatalf("replay: %v, printed %q, returned %s; want %q, %s", err, replayed, rres.Inspect(), printed, res.Inspect())
	}
	_, rres, err = replayLogged(t, `puts(getenv("ARV_P6_OTHER"))`, log, env)
	e, ok := rres.(*value.Error)
	want := `at task root, effect 1: the log has getenv("ARV_P6_GATE"), the program asked for getenv("ARV_P6_OTHER")`
	if !ok || e.ErrKind() != runtime.KindReplay || !strings.Contains(e.Message, want) || err == nil {
		t.Fatalf("an edited program: %s, %v", rres.Inspect(), err)
	}
}
