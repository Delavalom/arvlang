// Package check finds, without running a program, the grants it can need:
// the --allow set that arv check prints. It reads the syntax tree only. It
// never evaluates the program, so it never performs an effect.
package check

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/delavalom/arvlang/arv/ast"
	"github.com/delavalom/arvlang/arv/llm"
	"github.com/delavalom/arvlang/arv/runtime"
)

// effect describes a builtin that can be admitted for a grant: an effect
// (http_get, read_file, write_file, getenv), checked against the task's
// grants when it runs, or a capability constructor (net, fs_read,
// fs_write, env, llm, tool), whose value spawn ... with checks against the
// parent's.
type effect struct {
	kind    runtime.Kind
	nargs   int    // the arguments a valid call takes; the first names the scope
	maxArgs int    // if more than nargs, a valid call may take up to this many
	what    string // what that argument names, for a note: "host", "path", ...
	// scope turns the argument into the scope it needs, or fails as the
	// runtime would, before admitting anything.
	scope func(kind runtime.Kind, arg string) (string, error)
}

var effects = map[string]effect{
	"http_get":   {runtime.Net, 1, 0, "host", urlScope},
	"read_file":  {runtime.FSRead, 1, 0, "path", literalScope},
	"write_file": {runtime.FSWrite, 2, 0, "path", literalScope},
	"getenv":     {runtime.Env, 1, 0, "name", literalScope},
	"net":        {runtime.Net, 1, 0, "scope", literalScope},
	"fs_read":    {runtime.FSRead, 1, 0, "scope", literalScope},
	"fs_write":   {runtime.FSWrite, 1, 0, "scope", literalScope},
	"env":        {runtime.Env, 1, 0, "scope", literalScope},
	"llm":        {runtime.LLM, 1, 0, "scope", literalScope},
	"tool":       {runtime.Tool, 1, 0, "scope", literalScope},
	// ask and agent check the model's form (llm.ParseModel) before
	// requiring llm(model); agent first checks its tools (see agentTools in
	// check.go), and each tool the model calls needs tool(name).
	"ask":   {runtime.LLM, 2, 0, "model", modelScope},
	"agent": {runtime.LLM, 3, 4, "model", modelScope},
}

// modelScope is the llm scope ask or agent requires for model spec: one
// named model, as llm.ParseModel accepts it.
func modelScope(kind runtime.Kind, spec string) (string, error) {
	if _, _, err := llm.ParseModel(spec); err != nil {
		return "", err
	}
	return literalScope(kind, spec)
}

// pure are the builtins that need no grant. sleep, clock_ms and now read
// the clock, random draws a number and puts writes to the run's output,
// all without a capability. str renders a value as puts prints it, and
// $str (ast.StrBuiltin) is str under the name an interpolated string calls.
var pure = map[string]bool{
	"clock_ms": true, "error": true, "json": true, "last": true, "len": true,
	"lines": true, "push": true, "puts": true, "sleep": true, "split": true,
	"now": true, "random": true, "str": true, ast.StrBuiltin: true,
}

// literalScope is arg as a scope of kind, validated as runtime.Literal
// validates it. A net scope comes back normalised (lowercase, no trailing
// dot), as the runtime compares it. A path comes back as written: the
// program and --allow both resolve a relative path against the working
// directory, so the printed grant names the same file.
func literalScope(kind runtime.Kind, arg string) (string, error) {
	probe := arg
	isPath := kind == runtime.FSRead || kind == runtime.FSWrite
	if isPath && arg != "" && !filepath.IsAbs(arg) {
		probe = "/" + arg // Literal wants it absolute; the base does not matter
	}
	c, err := runtime.Literal(kind, probe)
	if err != nil {
		return "", err
	}
	if isPath {
		return arg, nil
	}
	return c.Scope, nil
}

// maxURLLen is http_get's limit on a URL.
const maxURLLen = 8 << 10

// defaultPorts are the ports a URL without one reaches, as in http_get.
var defaultPorts = map[string]string{"http": "80", "https": "443"}

// urlScope is the net scope http_get(arg) is checked against: the URL's
// host and the port it reaches, as http_get computes it before the first
// request. It does not follow redirects; each one is checked at run time.
func urlScope(_ runtime.Kind, arg string) (string, error) {
	if len(arg) > maxURLLen {
		return "", fmt.Errorf("URL is %d bytes long (max %d)", len(arg), maxURLLen)
	}
	u, err := url.Parse(arg)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("invalid url")
	}
	if u.Hostname() == "" {
		return "", errors.New("URL has no host")
	}
	port := u.Port()
	if port == "" {
		port = defaultPorts[u.Scheme]
	}
	return literalScope(runtime.Net, net.JoinHostPort(u.Hostname(), port))
}

// dialChecked are the effects whose connections the runtime checks again
// when they dial, on the address reached (runtime.Task.RequireDial).
var dialChecked = map[string]bool{"http_get": true}

// dialNeed is what a request to scope (host:port, normalised) needs at
// dial time beyond scope itself, when arv check can tell without resolving
// a name. For an IP literal it asks the runtime's own dial check: the
// unspecified address (0.0.0.0, ::) lands on loopback, so it also needs
// 127.0.0.1:port or [::1]:port. For a name that a resolver may read as an
// IP address (127.1, 2130706433, 0x7f.1), it returns a note instead: what
// that dial needs depends on the resolver, as for any name.
func dialNeed(scope string) (need, note string) {
	if ap, err := netip.ParseAddrPort(scope); err == nil {
		var de *runtime.DeniedError
		t := &runtime.Task{ID: "check"} // holds nothing, so the error names the need
		if errors.As(t.RequireDial(scope, ap), &de) &&
			!(runtime.Capability{Kind: runtime.Net, Scope: scope}).Covers(de.Want) {
			return de.Want.Scope, ""
		}
		return "", ""
	}
	host, _, err := net.SplitHostPort(scope)
	if err == nil && numericName(host) {
		return "", "host " + host + " is not an IP literal, but a resolver may read it as an IP address; " +
			"if it does, the dial also needs a grant for that address"
	}
	return "", ""
}

// numericName reports whether host, a name (not an IP literal), may be
// read as an IPv4 address by inet_aton: its last label is all digits (no
// TLD is) or hexadecimal with a 0x prefix.
func numericName(host string) bool {
	last := host[strings.LastIndex(host, ".")+1:]
	if hex, ok := strings.CutPrefix(last, "0x"); ok {
		return strings.Trim(hex, "0123456789abcdef") == ""
	}
	return last != "" && strings.Trim(last, "0123456789") == ""
}
