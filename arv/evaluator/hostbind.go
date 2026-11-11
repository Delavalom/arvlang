package evaluator

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/delavalom/arvlang/arv/lexer"
	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/token"
	"github.com/delavalom/arvlang/arv/value"
)

// Effect is an effect a Go host adds to a run (Phase 8.2). A call runs Fn
// only if the calling task holds a grant of Kind covering Scope(args);
// otherwise it is a capability error. HostBindings refuses an Effect
// without a Kind, a Scope or an Fn: nothing can add an unchecked effect.
type Effect struct {
	// Kind is the capability kind every call needs: a host kind
	// (runtime.Kinds), never a built-in one, whose checks (canonical paths,
	// dial-time address checks) hold only for the runtime's own effects.
	Kind runtime.Kind
	// Scope names the scope of Kind a call with args needs, e.g. the
	// namespace a manifest goes to. An error refuses the call as a
	// runtime error (the program passed bad arguments).
	Scope func(args []value.Object) (string, error)
	// Fn performs the effect, as the calling task t, once t's grants
	// admit the call. It must honour t.Ctx (the scope waits for it, so a
	// call that ignores cancellation delays the scope's end), must not
	// change args, and returns Arv data (see hostValue) or an error, which
	// becomes an Arv error ("name: message", of kind effect unless err
	// says otherwise, or cancelled once t is).
	Fn func(t *runtime.Task, args ...value.Object) (value.Object, error)
}

// MaxEffectName bounds a host effect's name.
const MaxEffectName = 64

// HostBindings validates a host's kinds and effects and returns what a
// run binds in its global environment: a capability constructor for each
// kind in kinds (k8s("staging"), like net("host")), and each effect under
// its name. A name must be an identifier, not a keyword and not a
// builtin's; an effect needs a declared host kind, a Scope and an Fn.
func HostBindings(kinds *runtime.Kinds, effects map[string]Effect) (map[string]*value.Builtin, error) {
	out := make(map[string]*value.Builtin)
	for _, kind := range kinds.List() {
		if err := checkHostName(string(kind)); err != nil {
			return nil, fmt.Errorf("custom kind %q: %w", kind, err)
		}
		out[string(kind)] = &value.Builtin{Fn: kindConstructor(kinds, kind)}
	}
	for _, name := range slices.Sorted(maps.Keys(effects)) {
		e := effects[name]
		if err := checkHostName(name); err != nil {
			return nil, fmt.Errorf("effect %q: %w", name, err)
		}
		var err error
		switch {
		case out[name] != nil:
			err = errors.New("a custom kind's constructor has that name")
		case e.Kind == "":
			err = errors.New("no capability kind: every host effect must name the kind it needs")
		case runtime.IsBuiltin(e.Kind):
			err = fmt.Errorf("kind %q is built in; a host effect needs a host kind", e.Kind)
		case !kinds.Has(e.Kind):
			err = fmt.Errorf("kind %q is not declared", e.Kind)
		case e.Scope == nil:
			err = errors.New("no Scope function")
		case e.Fn == nil:
			err = errors.New("no Fn")
		}
		if err != nil {
			return nil, fmt.Errorf("effect %q: %w", name, err)
		}
		out[name] = &value.Builtin{Charging: hostEffect(name, e)}
	}
	return out, nil
}

func checkHostName(name string) error {
	if name == "" {
		return errors.New("empty name")
	}
	if len(name) > MaxEffectName {
		return fmt.Errorf("name is longer than %d bytes", MaxEffectName)
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		letter := 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' || c == '_'
		if !letter && (i == 0 || c < '0' || c > '9') {
			return errors.New("want a letter or '_', then letters, digits or '_'")
		}
	}
	if lexer.LookupIdent(name) != token.IDENT {
		return errors.New("is a keyword")
	}
	if builtins[name] != nil {
		return errors.New("is a builtin")
	}
	return nil
}

// kindConstructor builds a host kind's capability values, as
// capabilityConstructor does net("host"): it names a grant and gives no
// authority until spawn ... with checks it.
func kindConstructor(kinds *runtime.Kinds, kind runtime.Kind) value.BuiltinFunction {
	return func(t *runtime.Task, args ...value.Object) value.Object {
		scope, errObj := oneString(string(kind), args)
		if errObj != nil {
			return errObj
		}
		c, err := kinds.Literal(kind, scope)
		if err != nil {
			return newError("%s", err)
		}
		return &value.Capability{Cap: c}
	}
}
