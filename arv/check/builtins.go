// Package check finds, without running a program, the grants it can need:
// the --allow set that arv check prints. It reads the syntax tree only. It
// never evaluates the program, so it never performs an effect.
package check

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"

	"github.com/delavalom/arvlang/arv/runtime"
)

// effect describes a builtin that can be admitted for a grant: an effect
// (http_get, read_file, write_file, getenv), checked against the task's
// grants when it runs, or a capability constructor (net, fs_read,
// fs_write, env), whose value spawn ... with checks against the parent's.
type effect struct {
	kind  runtime.Kind
	nargs int    // the arguments a valid call takes; the first names the scope
	what  string // what that argument names, for a note: "host", "path", ...
	// scope turns the argument into the scope it needs, or fails as the
	// runtime would, before admitting anything.
	scope func(kind runtime.Kind, arg string) (string, error)
}

var effects = map[string]effect{
	"http_get":   {runtime.Net, 1, "host", urlScope},
	"read_file":  {runtime.FSRead, 1, "path", literalScope},
	"write_file": {runtime.FSWrite, 2, "path", literalScope},
	"getenv":     {runtime.Env, 1, "name", literalScope},
	"net":        {runtime.Net, 1, "scope", literalScope},
	"fs_read":    {runtime.FSRead, 1, "scope", literalScope},
	"fs_write":   {runtime.FSWrite, 1, "scope", literalScope},
	"env":        {runtime.Env, 1, "scope", literalScope},
}

// pure are the builtins that need no grant. sleep and clock_ms read the
// clock and puts writes to the run's output, all without a capability.
var pure = map[string]bool{
	"clock_ms": true, "error": true, "json": true, "last": true, "len": true,
	"lines": true, "push": true, "puts": true, "sleep": true, "split": true,
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
