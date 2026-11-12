package e2e

import (
	"strconv"
	"testing"
)

// Phase 8.2 acceptance (15-phase8-tooling.md §8.2, as built in
// 16-phase82.md): a host effect is denied without its grant and allowed
// with it, and Fn is never called on a denial. "Recorded and replayed"
// waits for the Phase 6 gate (16-phase82.md, the seam).
func TestP82Accept(t *testing.T) {
	t.Parallel()
	const apply = `puts(k8s_apply("staging", "web.yaml")["applied"])`
	runHosts(t, []hostSpec{
		{name: "denied without a grant", src: apply, code: 1, kind: "capability",
			errHas: []string{`capability denied: k8s("staging") in task root (holds: [])`}},
		{name: "allowed with the grant", src: apply, caps: []string{"k8s=staging"},
			stdout: "web.yaml\n", calls: []string{"k8s_apply(staging) task=root"}},
		{name: "denied in a namespace not granted", caps: []string{"k8s=staging"},
			src: `k8s_apply("prod", "web.yaml")`, code: 1, kind: "capability",
			errHas: []string{`capability denied: k8s("prod") in task root (holds: [k8s("staging")])`}},
		{name: "a denial is an Arv error the program can match", caps: []string{"k8s=staging"},
			src:    `puts(match k8s_apply("prod", "m") { ok(v) => "applied", err(e) => [e["kind"], e["task"]] })`,
			stdout: "[capability, root]\n"},
		{name: "a built-in grant covers no host kind", caps: []string{"env=*", "net=*", "fs_read=/"},
			src: apply, code: 1, kind: "capability", errHas: []string{`capability denied: k8s("staging")`}},
		{name: "a host grant covers no built-in kind", caps: []string{"k8s=staging", "team=infra"},
			src:    `puts(match getenv("HOME") { ok(v) => "read", err(e) => e["kind"] })`,
			stdout: "capability\n"},
		{name: "a grant of one host kind covers no other", caps: []string{"team=staging"},
			src: apply, code: 1, kind: "capability", errHas: []string{`k8s("staging")`}},
	})
}

// Coverage, through the binary: k8s is exact match (no normalisation, no
// prefix), team is the host's subtree rule. Only the allowed calls reach
// Fn.
func TestP82Coverage(t *testing.T) {
	t.Parallel()
	const try = `let try = fn(f, s) { match f(s) { ok(v) => "ok", err(e) => e["kind"] } };` + "\n"
	runHosts(t, []hostSpec{
		{name: "k8s is exact match", caps: []string{"k8s=staging"},
			src: try + `let f = fn(s) { k8s_apply(s, "m") };
puts(for s in ["staging", "Staging", "staging/x", "stag", "staging ", " staging", "stagingx", "prod"] { try(f, s) })`,
			stdout: "[ok, capability, capability, capability, capability, capability, capability, capability]\n",
			calls:  []string{"k8s_apply(staging) task=root"}},
		{name: "team covers its subtree", caps: []string{"team=infra"},
			src:    try + `puts(for s in ["infra", "infra/db", "infra/db/eu", "infrastructure", "infra/", "ops", "INFRA", "infra/../ops"] { try(team_read, s) })`,
			stdout: "[ok, ok, ok, capability, ok, capability, capability, ok]\n",
			calls: []string{"team_read(infra) task=root", "team_read(infra/db) task=root",
				"team_read(infra/db/eu) task=root", "team_read(infra/) task=root", "team_read(infra/../ops) task=root"}},
		{name: "a scope is kept as written: no path cleaning", caps: []string{"team=infra/db"},
			src:    try + `puts(for s in ["infra/db/../web", "infra/./db", "infra//db", "infra/db"] { try(team_read, s) })`,
			stdout: "[ok, capability, capability, ok]\n",
			calls:  []string{"team_read(infra/db/../web) task=root", "team_read(infra/db) task=root"}},
		{name: "two grants of one kind", caps: []string{"k8s=staging", "k8s=prod"},
			src:    try + `let f = fn(s) { k8s_apply(s, "m") }; puts(for s in ["staging", "prod", "dev"] { try(f, s) })`,
			stdout: "[ok, ok, capability]\n",
			calls:  []string{"k8s_apply(prod) task=root", "k8s_apply(staging) task=root"}},
		{name: "an empty scope is a denial", caps: []string{"k8s=staging"},
			src: try + `let f = fn(s) { k8s_apply(s, "m") }; puts(try(f, ""))`, stdout: "capability\n"},
		{name: "a scope over MaxScopeLen is a runtime error", caps: []string{"k8s=staging"},
			src: `let grow = fn(s, n) { if (n == 0) { s } else { grow(s + s, n - 1) } };
puts(match k8s_apply(grow("x", 13), "m") { ok(v) => "ok", err(e) => e["kind"] })`,
			stdout: "runtime\n"},
		{name: "a bad scope argument is a runtime error before any check", caps: []string{"k8s=staging"},
			src: `k8s_apply(42, "m")`, code: 1, kind: "runtime",
			errHas: []string{"k8s_apply: the scope must be STRING, got INTEGER"}},
	})
}

// A setup that would register an unchecked effect, hide a builtin, or
// redefine a built-in kind is refused before the program runs: nothing
// is printed and no Fn is called.
func TestP82Registration(t *testing.T) {
	t.Parallel()
	var specs []hostSpec
	for _, tt := range []struct{ setup, want string }{
		{"nokind", `effect "k8s_raw": no capability kind`},
		{"noscope", `effect "k8s_raw": no Scope function`},
		{"nofn", `effect "k8s_raw": no Fn`},
		{"netkind", `effect "k8s_raw": kind "net" is built in`},
		{"envkind", `effect "k8s_raw": kind "env" is built in`},
		{"hidebuiltin", `effect "puts": is a builtin`},
		{"keyword", `effect "spawn": is a keyword`},
		{"ctorname", `effect "team": a custom kind's constructor has that name`},
		{"badname", `effect "k8s-raw"`},
		{"badkind", `custom kind "K8S"`},
		{"dupkind", `custom kind "team" declared twice`},
		{"builtinspec", `custom kind "fs_read" is built in`},
	} {
		specs = append(specs, hostSpec{name: tt.setup, flags: []string{"-setup", tt.setup},
			caps: []string{"k8s=staging"}, src: `puts("ran"); k8s_apply("staging", "m")`,
			code: 1, errHas: []string{"error: arv: " + tt.want}})
	}
	specs = append(specs,
		hostSpec{name: "a grant of an undeclared kind", caps: []string{"queue=q"},
			src: `puts("ran")`, code: 1, errHas: []string{"unknown capability kind"}},
		hostSpec{name: "an empty host grant", caps: []string{"k8s="},
			src: `puts("ran")`, code: 1, errHas: []string{"empty scope"}},
	)
	runHosts(t, specs)
}

// A kind's constructor names a grant; holding the value gives nothing.
func TestP82CapabilityValues(t *testing.T) {
	t.Parallel()
	runHosts(t, []hostSpec{
		{name: "constructors make values", src: `puts([k8s("staging"), team("infra/db")])`,
			stdout: "[k8s(\"staging\"), team(\"infra/db\")]\n"},
		{name: "a value is not a grant", src: `let c = k8s("prod"); k8s_apply("prod", "m")`,
			code: 1, kind: "capability", errHas: []string{`k8s("prod") in task root (holds: [])`}},
		// I1 (mutation M07): a value passed to an effect is data, not a
		// grant, at the root or in a child spawned without `with`. Fn
		// never runs (calls is nil).
		{name: "a value passed to an effect is not a grant", src: `k8s_echo("prod", k8s("prod"))`,
			code: 1, kind: "capability", errHas: []string{`k8s("prod") in task root (holds: [])`}},
		{name: "a value passed to a child is not a grant", caps: []string{"k8s=prod"},
			src:  `let f = fn(c) { k8s_echo("prod", c) }; await spawn f(k8s("prod"))`,
			code: 1, kind: "capability", errHas: []string{`k8s("prod") in task root/0 (holds: [])`}},
		{name: "an empty scope is refused", src: `k8s("")`, code: 1, errHas: []string{"k8s: empty scope"}},
		{name: "a host kind's value is refused where a built-in grant is wanted", caps: []string{"k8s=staging"},
			src:    `let f = fn() { match getenv("HOME") { ok(v) => v, err(e) => e["kind"] } }; puts(await spawn f() with [k8s("staging")])`,
			stdout: "capability\n"},
	})
}

// The host holds a secret it uses inside Fn; the program sees only what
// Fn returns, and a result that is not data (a builtin returning the
// secret) is refused.
func TestP82HostSecret(t *testing.T) {
	t.Parallel()
	const secret = "sk-p82-host-secret"
	runHosts(t, []hostSpec{
		{name: "an effect uses the secret for the program", caps: []string{"k8s=staging"}, gates: true,
			src: `puts(k8s_call("staging"))`, stdout: "answered " + strconv.Itoa(len(okBody)) + " bytes\n",
			errNot: []string{secret}, calls: []string{"k8s_call(staging) task=root"}},
		{name: "a builtin result is refused", caps: []string{"k8s=staging"},
			src: `let f = k8s_func("staging"); f()`, code: 1, kind: "runtime",
			errHas: []string{"k8s_func:", "BUILTIN"}, errNot: []string{secret},
			calls: []string{"k8s_func(staging) task=root"}},
		{name: "a denial lists grants, never host state", caps: []string{"k8s=staging"},
			src: `k8s_call("prod")`, code: 1, kind: "capability", errNot: []string{secret}},
	})
}

// The arv CLI knows no host kind or effect: a host kind cannot be granted
// with --allow, and its constructor and effects are unbound names.
func TestP82CLIHasNoHostKinds(t *testing.T) {
	t.Parallel()
	runInline(t, []inline{
		{name: "a host effect is unbound", src: `k8s_apply("staging", "m")`, err: "identifier not found: k8s_apply", at: "1:1"},
		{name: "a host kind's constructor is unbound", src: `k8s("staging")`, err: "identifier not found: k8s", at: "1:1"},
		{name: "--allow refuses a host kind", src: `puts("ran")`, flags: []string{"--allow", "k8s=staging"}, usage: true,
			err: `unknown capability kind "k8s"`},
	})
}
