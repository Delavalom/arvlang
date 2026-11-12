package runtime

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

// replayer reads data back and starts a replay of it printing to out.
func replayer(t *testing.T, data []byte, out *bytes.Buffer) (*EffectLog, *Task) {
	t.Helper()
	log, err := ReadLog(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewReplayer(log, out)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(l.Start(context.Background()), NewCapSet(), out)
	root.SetLog(l)
	return l, root
}

// recordRun records a small run: root makes two effects, root/0 one,
// and the run ends with result.
func recordRun(t *testing.T, result string) []byte {
	t.Helper()
	var log bytes.Buffer
	l, err := NewRecorder(&log, Header{ScriptSHA256: "abc"}, &bytes.Buffer{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	root := NewRoot(l.Start(context.Background()), NewCapSet(), &bytes.Buffer{})
	root.SetLog(l)
	for _, req := range []string{`["a"]`, `["b"]`} {
		if _, err := root.Effect("getenv", []byte(req), func() Outcome { return Outcome{Res: []byte(`"v"`), Charged: 1} }); err != nil {
			t.Fatal(err)
		}
	}
	child := root.Child(root.Ctx, NewCapSet())
	if _, err := child.Effect("sleep", []byte(`[5]`), func() Outcome { return Outcome{Res: []byte(`null`)} }); err != nil {
		t.Fatal(err)
	}
	if err := l.Finish([]byte(result)); err != nil {
		t.Fatal(err)
	}
	return log.Bytes()
}

func never(t *testing.T) func() Outcome {
	return func() Outcome { t.Error("a replayed effect ran live"); return Outcome{} }
}

// I7: a replay answers every effect from the log and runs none of them.
func TestReplayAnswersFromTheLog(t *testing.T) {
	data := recordRun(t, `[7]`)
	var out bytes.Buffer
	l, root := replayer(t, data, &out)
	for _, req := range []string{`["a"]`, `["b"]`} {
		o, err := root.Effect("getenv", []byte(req), never(t))
		if err != nil || !o.Replayed || string(o.Res) != `"v"` || o.Charged != 1 {
			t.Fatalf("Effect(%s) = %+v, %v", req, o, err)
		}
	}
	child := root.Child(root.Ctx, NewCapSet())
	if o, err := child.Effect("sleep", []byte(`[5]`), never(t)); err != nil || string(o.Res) != "null" {
		t.Fatalf("child = %+v, %v", o, err)
	}
	if err := l.Finish([]byte(`[7]`)); err != nil {
		t.Fatalf("Finish = %v", err)
	}
}

func TestReplayDiverges(t *testing.T) {
	data := recordRun(t, `[7]`)
	for _, tt := range []struct {
		name string
		run  func(l *EffectLog, root *Task) error
		want string
	}{
		{"another request", func(l *EffectLog, root *Task) error {
			_, err := root.Effect("getenv", []byte(`["z"]`), never(t))
			return err
		}, `at task root, effect 1: the log has getenv("a"), the program asked for getenv("z")`},
		{"another effect", func(l *EffectLog, root *Task) error {
			_, err := root.Effect("now", []byte(`[]`), never(t))
			return err
		}, `the log has getenv("a"), the program asked for now()`},
		{"past the log", func(l *EffectLog, root *Task) error {
			_, err := root.Child(root.Ctx, NewCapSet()).Child(root.Ctx, NewCapSet()).Effect("now", []byte(`[]`), never(t))
			return err
		}, "at task root/0/0, effect 1: the log has no more records for it; the program asked for now()"},
		{"records left", func(l *EffectLog, root *Task) error { return l.Finish([]byte(`[7]`)) },
			`at task root: the log has 2 more records for it, from getenv("a")`},
		{"another result", func(l *EffectLog, root *Task) error {
			for _, r := range []string{`["a"]`, `["b"]`} {
				root.Effect("getenv", []byte(r), never(t))
			}
			root.Child(root.Ctx, NewCapSet()).Effect("sleep", []byte(`[5]`), never(t))
			return l.Finish([]byte(`[8]`))
		}, "at the end: the recorded run ended with [7], this run with [8]"},
	} {
		l, root := replayer(t, data, &bytes.Buffer{})
		err := tt.run(l, root)
		var d *DivergedError
		if !errors.As(err, &d) || !strings.Contains(err.Error(), tt.want) || ErrorKind(err, "") != KindReplay {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
		if root.Ctx.Err() == nil {
			t.Errorf("%s: a divergence did not abort the run", tt.name)
		}
		if _, err2 := root.Effect("now", nil, never(t)); err2 != err && !errors.As(err2, &d) {
			t.Errorf("%s: after diverging, Effect = %v", tt.name, err2)
		}
	}
}
