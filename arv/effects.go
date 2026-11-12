package arv

import (
	"maps"
	"slices"

	"github.com/delavalom/arvlang/arv/evaluator"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// Effect is a Go host's own effect (Phase 8.2): Options.Effects binds it
// under a name, and every call needs a grant of its Kind covering
// Scope(args) before Fn runs. See evaluator.Effect.
type Effect = evaluator.Effect

// hostSetup declares the host's kinds (opts.Kinds, plus, with exact-match
// coverage, every effect's kind declared nowhere else) and returns them
// with the builtins Run binds: each effect, and a constructor per kind.
// An effect whose kind is empty or built in is left to HostBindings,
// which refuses it by name.
func hostSetup(opts Options) (*runtime.Kinds, map[string]*value.Builtin, error) {
	if len(opts.Kinds) == 0 && len(opts.Effects) == 0 {
		return nil, nil, nil
	}
	specs := slices.Clone(opts.Kinds)
	declared := make(map[runtime.Kind]bool, len(specs))
	for _, s := range specs {
		declared[s.Kind] = true
	}
	for _, name := range slices.Sorted(maps.Keys(opts.Effects)) {
		k := opts.Effects[name].Kind
		if k != "" && !declared[k] && !runtime.IsBuiltin(k) {
			declared[k] = true
			specs = append(specs, runtime.KindSpec{Kind: k})
		}
	}
	kinds, err := runtime.NewKinds(specs...)
	if err != nil {
		return nil, nil, err
	}
	host, err := evaluator.HostBindings(kinds, opts.Effects)
	if err != nil {
		return nil, nil, err
	}
	return kinds, host, nil
}
