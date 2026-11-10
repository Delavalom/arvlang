# Phase 8: Tooling, extensibility, hardening

**Status:** a menu, not a sequence. Each item is independent and gets its own small plan. Pick by what Phases 5–7 showed was missing.

## 8.1 `arv check`: know the grants before running

`arv check script.arv` walks the AST and prints the **least `--allow` set** the script can need: every capability constructor with a literal argument, every `spawn … with [...]`, and every effect call with a literal URL or path. When arguments aren't literal, it says so (`http_get at line 14: host not statically known`). Optional header comments make the manifest explicit:

```arv
// arv:allow net=api.github.com
// arv:allow fs_write=./out
```

`arv run --manifest` grants exactly the header's set, and refuses to run if `arv check` finds a need outside it. **Acceptance:** for every file in `examples/`, the printed set is exactly the flags its README line uses.

## 8.2 Custom effects for Go hosts (the extensibility story)

```go
arv.Run(src, arv.Options{
	Caps: []runtime.Capability{{Kind: "k8s", Scope: "staging"}},
	Effects: map[string]arv.Effect{
		"k8s_apply": {
			Kind:  "k8s",
			Scope: func(args []value.Object) string { return args[0].(*value.String).Value }, // namespace
			Fn:    applyManifest, // func(t *runtime.Task, args ...value.Object) value.Object
		},
	},
})
```

- `runtime` gets a registry for custom kinds (exact-match coverage by default, or a host-supplied `Covers` function).
- A custom effect declares which capability it needs, and the runtime checks it before calling `Fn`. A host can't accidentally add an unchecked effect, because there's no way to register one without a kind.
- Custom effects go through the Phase 6 gate automatically.

**Acceptance:** an example host in `examples/host/` with a `k8s_apply` fake: denied without the grant, allowed with it, recorded and replayed.

## 8.3 Network hardening

- **Resolved-IP check:** a custom `DialContext` refuses loopback, private (RFC 1918), link-local and metadata (`169.254.169.254`) addresses unless the grant itself is an IP or CIDR (`net=10.0.0.0/8`). This closes DNS-rebinding and SSRF through a granted hostname.
- Ports: `net("host:443")` restricts ports. A bare host keeps meaning any port.
- **Acceptance:** a grant for `rebind.test`, served by a local DNS stub resolving to `127.0.0.1`, is refused; `net=127.0.0.1` still works for tests.

## 8.4 Speed (only if the gate said CPU matters)

Compile the AST to Go closures once (`func(env) value.Object` per node) instead of re-walking it. That usually gives 2–5× on tree-walkers with no change in semantics. Keep the tree-walker as the reference and run the whole suite against both (differential test). A bytecode VM only if closures aren't enough.

## 8.5 Distribution

- Move `main.go` to `cmd/arv/`, so `go install github.com/delavalom/arvlang/cmd/arv@latest` produces a binary named `arv`.
- goreleaser for macOS/Linux binaries; `arv version` prints the git SHA.

## 8.6 Editor support (optional)

`arv fmt` (an AST printer that keeps comments; it needs comment tokens kept instead of skipped), then a minimal LSP: diagnostics from the parser plus `arv check` grant hints on hover.

## 8.7 Running untrusted Wasm as an effect (optional, research)

Use `wazero` (pure-Go WebAssembly runtime) to run a Wasm module as an effect whose host imports are capability-checked the same way. That would let Arv orchestrate code written in other languages with the same guarantees.
