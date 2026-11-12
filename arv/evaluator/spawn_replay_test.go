package evaluator

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Canonicalising a spawn's grants reads the filesystem, so it is an
// effect: a replay takes the child's grants from the log, and checks each
// against the task's own, so a forged log cannot widen them (I2).
func TestReplayedSpawnGrants(t *testing.T) {
	a, _ := runtime.New(runtime.Env, "ARV_P6_A")
	src := `let f = fn() { 1 }; await spawn f() with [env("ARV_P6_A")]`
	_, res, log := recordLogged(t, src, a)
	if res.Inspect() != "1" || !bytes.Contains(log, []byte(`"kind":"spawn","req":["env(\"ARV_P6_A\")"],"res":[["env","ARV_P6_A"]]`)) {
		t.Fatalf("recording returned %s, logged\n%s", res.Inspect(), log)
	}
	if _, rres, err := replayLogged(t, src, log, a); err != nil || rres.Inspect() != "1" {
		t.Fatalf("replay: %s, %v", rres.Inspect(), err)
	}

	var forged bytes.Buffer
	l, _ := runtime.NewRecorder(&forged, runtime.Header{}, &bytes.Buffer{}, nil)
	root := runtime.NewRoot(l.Start(context.Background()), runtime.NewCapSet(a), &bytes.Buffer{})
	root.SetLog(l)
	root.Effect("spawn", []byte(`["env(\"ARV_P6_A\")"]`), func() runtime.Outcome {
		return runtime.Outcome{Res: []byte(`[["env","ARV_P6_B"]]`)}
	})
	l.Finish([]byte(`[1]`))
	_, rres, err := replayLogged(t, src, forged.Bytes(), a)
	e, ok := rres.(*value.Error)
	want := `the log's grants for a spawn include env("ARV_P6_B"), which the task does not hold`
	if !ok || e.ErrKind() != runtime.KindReplay || !strings.Contains(e.Message, want) || err == nil {
		t.Fatalf("a forged log: %s, %v", rres.Inspect(), err)
	}
}
