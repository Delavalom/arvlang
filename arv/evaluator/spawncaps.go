package evaluator

import (
	"errors"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// attenuate is the child's grants for spawn ... with reqs (I2): each
// request canonicalised and checked against the task's own grants.
// Canonicalising reads the filesystem (lstat, readlink), which is OS input,
// so with an effect log it is an effect named "spawn" (I7): a replay gets
// the canonical grants, or the error, from the log, and checks that the
// task holds every grant the log gives the child, so a forged log cannot
// widen it.
func (in *Interpreter) attenuate(reqs []value.Object) (runtime.CapSet, *value.Error) {
	if in.Task.Log() == nil || len(reqs) == 0 {
		return in.attenuateLive(reqs, in.chargeBytes)
	}
	var caps runtime.CapSet
	ran := false
	names := make([]value.Object, len(reqs))
	for i, r := range reqs {
		names[i] = &value.String{Value: r.(*value.Capability).Cap.String()}
	}
	result := in.effect("spawn", names, func(charge func(int64) error) value.Object {
		ran = true
		c, errv := in.attenuateLive(reqs, charge)
		if errv != nil {
			return errv
		}
		caps = c
		list := c.List()
		els := make([]value.Object, len(list))
		for i, g := range list {
			els[i] = &value.Array{Elements: []value.Object{&value.String{Value: string(g.Kind)}, &value.String{Value: g.Scope}}}
		}
		return &value.Array{Elements: els}
	})
	if e, ok := result.(*value.Error); ok {
		return runtime.CapSet{}, e
	}
	if ran {
		return caps, nil
	}
	return in.loggedCaps(result)
}

// loggedCaps is the grants a replayed spawn's log gives the child, each
// checked: a valid capability, already canonical, held by the task. A
// grant of a host kind (Phase 8.2) is checked by the run's kinds, and the
// child keeps them, as Attenuate's sets do: otherwise a replayed spawn
// with k8s("staging") diverges, and a child replayed in a resume could
// not call its host effect live.
func (in *Interpreter) loggedCaps(v value.Object) (runtime.CapSet, *value.Error) {
	bad := func(what string) (runtime.CapSet, *value.Error) {
		err := in.Task.Log().Diverge("at task %s: the log's grants for a spawn %s", in.Task.ID, what)
		return runtime.CapSet{}, errorOf(err, runtime.KindReplay)
	}
	arr, ok := v.(*value.Array)
	if !ok {
		return bad("are not a list")
	}
	kinds := in.Task.Caps.Kinds()
	caps := make([]runtime.Capability, 0, len(arr.Elements))
	for _, el := range arr.Elements {
		pair, ok := el.(*value.Array)
		if !ok || len(pair.Elements) != 2 {
			return bad("are malformed")
		}
		kind, ok1 := pair.Elements[0].(*value.String)
		scope, ok2 := pair.Elements[1].(*value.String)
		if !ok1 || !ok2 {
			return bad("are malformed")
		}
		c, err := kinds.Literal(runtime.Kind(kind.Value), scope.Value)
		if err != nil || c.Scope != scope.Value || !in.Task.Caps.Allows(c) {
			return bad("include " + runtime.Capability{Kind: runtime.Kind(kind.Value), Scope: scope.Value}.String() + ", which the task does not hold")
		}
		caps = append(caps, c)
	}
	return runtime.NewCapSetKinds(kinds, caps...), nil
}

// attenuateLive canonicalises and checks reqs. Each request is
// canonicalised (reading the filesystem, O(depth x length) bytes) into a
// new scope the child keeps: charge both, and stop once the task is
// cancelled, since a program can pass any number of requests. It runs
// only live (inside the gate), so it looks at the task's context directly.
func (in *Interpreter) attenuateLive(reqs []value.Object, charge func(int64) error) (runtime.CapSet, *value.Error) {
	var stop *value.Error
	req := func(i int) runtime.Capability { return reqs[i].(*value.Capability).Cap }
	charged := func(n int64) error {
		if err := charge(n); err != nil {
			stop = errorOf(err, runtime.KindBudget)
			return errors.New(stop.Message)
		}
		return nil
	}
	caps, err := in.Task.Caps.AttenuateEach(len(reqs), req, charged, func(c runtime.Capability) error {
		if err := in.Task.LiveErr(); err != nil {
			stop = newErrorKind(runtime.KindCancelled, "spawn in task %s cancelled: %s", in.Task.ID, err)
			return errors.New(stop.Message)
		}
		return charged(capabilitySize + int64(len(c.Scope)))
	})
	if stop != nil {
		return runtime.CapSet{}, stop
	}
	if err != nil { // denied, or an OS error canonicalising inside a grant
		return runtime.CapSet{}, newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "spawn in task %s: %s", in.Task.ID, err)
	}
	return caps, nil
}
