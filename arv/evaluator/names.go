package evaluator

import "sort"

// BuiltinNames returns the name of every builtin a program can call,
// sorted. arv check (package check) classifies each one by the grant it
// needs, and its tests fail when a builtin is added here without a
// classification there.
func BuiltinNames() []string {
	names := make([]string, 0, len(builtins))
	for name := range builtins {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
