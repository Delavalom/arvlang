package evaluator

import "testing"

func TestValidSuperviseOptions(t *testing.T) {
	for _, src := range []string{
		`scope (supervise: "fail_fast") { 1 }`,
		`scope (supervise: "collect", tasks: 2) { 1 }`,
		`scope (supervise: "restart") { 1 }`,
		`scope (supervise: "restart", retries: 1000, backoff_ms: 30000, retry_on: ["effect", "user"]) { 1 }`,
		`scope (retry_on: ["user"], supervise: "restart", backoff_ms: 1) { 1 }`, // any order
	} {
		if got, _ := runWith(t, src); got.Inspect() != "1" {
			t.Errorf("%s = %s, want 1", src, got.Inspect())
		}
	}
}

// Bad supervision options fail before the body runs, at the option's name.
func TestSuperviseOptionErrors(t *testing.T) {
	for _, tt := range []struct{ input, msg, pos string }{
		{`scope (supervise: "retry") { 1 }`, `scope option supervise: want "fail_fast", "collect" or "restart", got "retry"`, "1:8"},
		{`scope (supervise: 1) { 1 }`, `scope option supervise: want "fail_fast", "collect" or "restart", got INTEGER`, "1:8"},
		{`scope (supervise: "collect", supervise: "restart") { 1 }`, `duplicate scope option "supervise"`, "1:30"},
		{`scope (retries: 2) { 1 }`, `scope option retries needs supervise: "restart"`, "1:8"},
		{`scope (supervise: "collect", backoff_ms: 5) { 1 }`, `scope option backoff_ms needs supervise: "restart"`, "1:30"},
		{`scope (retry_on: ["user"]) { 1 }`, `scope option retry_on needs supervise: "restart"`, "1:8"},
		{`scope (supervise: "restart", retries: 0) { 1 }`, "scope option retries: want a positive INTEGER, got 0", "1:30"},
		{`scope (supervise: "restart", retries: 1001) { 1 }`, "scope option retries: 1001 is too large (max 1000)", "1:30"},
		{`scope (supervise: "restart", backoff_ms: 30001) { 1 }`, "scope option backoff_ms: 30001 is too large (max 30000)", "1:30"},
		{`scope (supervise: "restart", retry_on: "user") { 1 }`, `scope option retry_on: want an ARRAY of error kinds, such as ["effect", "user"], got "user"`, "1:30"},
		{`scope (supervise: "restart", retry_on: []) { 1 }`, `scope option retry_on: the array is empty; name at least one error kind, such as ["effect", "user"]`, "1:30"},
		{`scope (supervise: "restart", retry_on: [1]) { 1 }`, `scope option retry_on: INTEGER is not a kind a restart retries (want "effect" or "user")`, "1:30"},
		{`scope (supervise: "restart", retry_on: ["effect", "capability"]) { 1 }`, `scope option retry_on: "capability" is not a kind a restart retries (want "effect" or "user")`, "1:30"},
		{`scope (supervise: "restart", retry_on: ["budget"]) { 1 }`, `scope option retry_on: "budget" is not a kind a restart retries (want "effect" or "user")`, "1:30"},
	} {
		got, out := runWith(t, tt.input+`; puts("after")`)
		wantErrorAt(t, got, tt.msg, tt.pos)
		if out != "" {
			t.Errorf("%s: printed %q after a bad option", tt.input, out)
		}
	}
}
