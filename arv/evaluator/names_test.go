package evaluator

import (
	"sort"
	"testing"
)

func TestBuiltinNamesListsEveryBuiltin(t *testing.T) {
	names := BuiltinNames()
	if len(names) != len(builtins) {
		t.Fatalf("BuiltinNames has %d names, the table %d", len(names), len(builtins))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("BuiltinNames is not sorted: %v", names)
	}
	for _, want := range []string{"http_get", "read_file", "write_file", "getenv", "net", "len", "puts"} {
		i := sort.SearchStrings(names, want)
		if i == len(names) || names[i] != want {
			t.Errorf("BuiltinNames lacks %q", want)
		}
	}
}
