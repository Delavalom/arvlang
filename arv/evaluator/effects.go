package evaluator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxBody caps how much of an HTTP response or file an effect will read.
// A larger body is an error, never silently truncated.
const maxBody = 10 << 20

var errTooBig = fmt.Errorf("body exceeds %d MiB limit", maxBody>>20)

// readChunk is how much readCapped reads between two charges.
const readChunk = 32 << 10

// maxURLLen caps http_get's URL, in bytes, as most servers do (nginx
// and Apache refuse a request line over about 8 KiB). Parsing a URL,
// building a request and an error naming it each copy it, uncharged.
const maxURLLen = 8 << 10

// clipLen is how much of an argument an error message quotes.
const clipLen = 256

// quoteClipped quotes s for an error message. Past clipLen bytes it
// quotes only the start (cut at a rune boundary) and gives the length, so
// an error, which is never charged to alloc_mb, stays small.
func quoteClipped(s string) string {
	if len(s) <= clipLen {
		return strconv.Quote(s)
	}
	i := clipLen
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return fmt.Sprintf("%q... (%d bytes)", s[:i], len(s))
}

// readCapped reads all of r, failing with errTooBig past maxBody bytes. It
// reads one byte more than the cap to tell "exactly maxBody" from "more".
// It charges each read to charge (alloc_mb; nil charges nothing) before
// keeping it, and stops with charge's error, so a reader holds at most one
// uncharged chunk: readers running at once under one scope stop together
// near its limit, not each at maxBody. It reads into fixed chunks and
// copies them once into a string of exactly the result's size, so the
// result keeps no slack alive (an Arv string holds its whole buffer, and
// slack is never charged), and a read allocates about twice its result.
func readCapped(r io.Reader, charge func(int64) error) (string, error) {
	r = io.LimitReader(r, maxBody+1)
	var chunks [][]byte
	total := 0
	for {
		if len(chunks) == 0 || len(chunks[len(chunks)-1]) == readChunk {
			chunks = append(chunks, make([]byte, 0, readChunk))
		}
		c := chunks[len(chunks)-1]
		n, err := r.Read(c[len(c):readChunk])
		if n > 0 {
			if charge != nil {
				if cerr := charge(int64(n)); cerr != nil {
					return "", cerr
				}
			}
			chunks[len(chunks)-1] = c[:len(c)+n]
			total += n
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
	}
	if total > maxBody {
		return "", errTooBig
	}
	var sb strings.Builder
	sb.Grow(total)
	for _, c := range chunks {
		sb.Write(c)
	}
	return sb.String(), nil
}

// readError is the error for a failed read by fn: a spent budget as
// itself, like every budget error, anything else prefixed with what.
func readError(what string, err error) *value.Error {
	var exceeded *runtime.ExceededError
	if errors.As(err, &exceeded) {
		return newError("%s", err)
	}
	return newError("%s: %s", what, err)
}

// absPath makes a script's path absolute against the working directory
// without cleaning it, so Canonical applies "." and ".." after symlinks, the
// same rule the CLI uses for relative grants (runtime.Resolve).
func absPath(raw string) (string, error) {
	if filepath.IsAbs(raw) {
		return raw, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return runtime.Resolve(cwd, raw), nil
}

// checkRegular returns f if it is a regular file, else closes it and fails.
// read_file reads only regular files: a FIFO or device could block or never end.
func checkRegular(f *os.File) (*os.File, error) {
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		what := "special file"
		switch t := fi.Mode().Type(); {
		case t&fs.ModeDir != 0:
			what = "directory"
		case t&fs.ModeNamedPipe != 0:
			what = "named pipe"
		case t&fs.ModeSocket != 0:
			what = "socket"
		case t&fs.ModeDevice != 0:
			what = "device"
		}
		err = fmt.Errorf("%s: not a regular file (%s)", f.Name(), what)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	return f, nil
}

// maxRedirects matches net/http's default limit.
const maxRedirects = 10

// clientFor returns an HTTP client that re-checks the task's net capability
// (host and port, see netRequest) on every redirect, so a granted host
// cannot bounce the task to another host or port, and checks every address
// it connects to (transportFor). The caller must CloseIdleConnections.
func clientFor(t *runtime.Task) *http.Client {
	return &http.Client{
		Transport: transportFor(t),
		Timeout:   30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			want, err := netRequest(req.URL)
			if err != nil {
				return err
			}
			return t.Require(want)
		},
	}
}

func init() {
	for name, kind := range map[string]runtime.Kind{
		"net": runtime.Net, "fs_read": runtime.FSRead, "fs_write": runtime.FSWrite, "env": runtime.Env,
	} {
		builtins[name] = &value.Builtin{Fn: capabilityConstructor(kind)}
	}
	builtins["http_get"] = &value.Builtin{Charging: httpGet}
	builtins["read_file"] = &value.Builtin{Charging: readFile}
	builtins["write_file"] = &value.Builtin{Fn: writeFile}
	builtins["getenv"] = &value.Builtin{Fn: getenv}
	builtins["sleep"] = &value.Builtin{Fn: sleep}
}

// capabilityConstructor builds net("host"), fs_read("path"), ... values.
// Relative paths resolve against the working directory (see absPath); an
// empty one is left for runtime.Literal to reject. The value holds the path
// as written, made absolute: building it needs no authority, so it must not
// touch the filesystem (I1). spawn canonicalises it (CapSet.Attenuate).
func capabilityConstructor(kind runtime.Kind) value.BuiltinFunction {
	return func(t *runtime.Task, args ...value.Object) value.Object {
		scope, errObj := oneString(string(kind), args)
		if errObj != nil {
			return errObj
		}
		if err := runtime.CheckScopeLen(kind, scope); err != nil {
			return newError("%s", err) // before absPath copies it
		}
		if (kind == runtime.FSRead || kind == runtime.FSWrite) && scope != "" {
			abs, err := absPath(scope)
			if err != nil {
				return newError("%s: %s", kind, err)
			}
			scope = abs
		}
		c, err := runtime.Literal(kind, scope)
		if err != nil {
			return newError("%s", err)
		}
		return &value.Capability{Cap: c}
	}
}

// httpGet charges the body as it reads it, then its record's two pairs
// (allocSize), as a ChargingFunction.
func httpGet(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	raw, errObj := oneString("http_get", args)
	if errObj != nil {
		return errObj
	}
	if len(raw) > maxURLLen { // before url.Parse and the errors copy it
		return newError("http_get: URL is %d bytes long (max %d)", len(raw), maxURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return newError("http_get: invalid url %s", quoteClipped(raw))
	}
	if u.Hostname() == "" {
		return newError("http_get: URL has no host")
	}
	want, err := netRequest(u)
	if err != nil {
		return newError("http_get: %s", err)
	}
	if err := t.Require(want); err != nil {
		return newError("%s", err)
	}
	req, err := http.NewRequestWithContext(t.Ctx, http.MethodGet, raw, nil)
	if err != nil {
		return newError("http_get: %s", err)
	}
	client := clientFor(t)
	defer client.CloseIdleConnections()
	resp, err := client.Do(req)
	if err != nil {
		var denied *runtime.DeniedError
		if errors.As(err, &denied) {
			return newError("%s", denied)
		}
		return newError("http_get: %s", err)
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body, charge)
	if errors.Is(err, errTooBig) {
		return newError("http_get: %s", err)
	}
	if err != nil {
		return readError("http_get: reading body", err)
	}
	if err := charge(2 * hashPairSize); err != nil {
		return newError("%s", err)
	}
	return value.NewHash(map[string]value.Object{
		"status": &value.Integer{Value: int64(resp.StatusCode)},
		"body":   &value.String{Value: body},
	})
}

// readFile charges the file's bytes as it reads them, as a
// ChargingFunction.
func readFile(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	tg, errObj := fsRequest(t, "read_file", runtime.FSRead, args)
	if errObj != nil {
		return errObj
	}
	f, err := openRegularIn(tg)
	if err != nil {
		return newError("read_file: %s", err)
	}
	defer f.Close()
	data, err := readCapped(f, charge)
	if err != nil {
		return readError("read_file", err)
	}
	return &value.String{Value: data}
}

func writeFile(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 2 {
		return newError("write_file: want 2 arguments (path, text), got %d", len(args))
	}
	text, ok := args[1].(*value.String)
	if !ok {
		return newError("write_file: text must be STRING, got %s", args[1].Type())
	}
	tg, errObj := fsRequest(t, "write_file", runtime.FSWrite, args[:1])
	if errObj != nil {
		return errObj
	}
	if err := writeIn(tg, text.Value); err != nil {
		return newError("write_file: %s", err)
	}
	return NIL
}

// writeIn is os.WriteFile(path, data, 0o644) for regular files only (see
// openWritableIn): it refuses a FIFO or device without blocking on it or
// truncating it. tg.path is canonical and covered by tg.grant (fsRequest),
// so it had no symlinks when checked; openIn keeps the open inside the
// grant even if a directory or the leaf is swapped for a symlink after that
// check. It takes the text as a string and writes it with WriteString,
// which does not copy it: a []byte conversion copied it on every call,
// uncharged.
func writeIn(tg fsTarget, data string) error {
	f, err := openWritableIn(tg, 0o644)
	if err != nil {
		return err
	}
	_, err = f.WriteString(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

func getenv(t *runtime.Task, args ...value.Object) value.Object {
	name, errObj := oneString("getenv", args)
	if errObj != nil {
		return errObj
	}
	want, err := runtime.New(runtime.Env, name)
	if err != nil {
		return newError("getenv: %s", err)
	}
	if err := t.Require(want); err != nil {
		return newError("%s", err)
	}
	return &value.String{Value: os.Getenv(name)}
}

// fsRequest resolves the path argument and checks the task may touch it.
// The path is made absolute (not canonical) and admitted like a spawn
// request (Task.Admit): it is canonicalised, so the check is on the path
// the OS will reach, but a denial, or a failure to canonicalise, names only
// the path as written. Neither a symlink target nor an lstat error outside
// the task's grants may leak (I1). It returns the canonical path, the
// scope of the outermost grant covering it (CapSet.Grant) and the
// directory the open must start from (confiningRoot, over the fs grants of
// the task and of every ancestor, Task.LineageCaps): the caller must open
// that path beneath that grant (openIn), so the check and the use agree
// (I3) even if the tree changes in between.
func fsRequest(t *runtime.Task, fn string, kind runtime.Kind, args []value.Object) (fsTarget, *value.Error) {
	raw, errObj := oneString(fn, args)
	if errObj != nil {
		return fsTarget{}, errObj
	}
	if raw == "" {
		return fsTarget{}, newError("%s: empty path", fn)
	}
	if err := runtime.CheckScopeLen(kind, raw); err != nil {
		return fsTarget{}, newError("%s: %s", fn, err) // before absPath copies it
	}
	abs, err := absPath(raw)
	if err != nil {
		return fsTarget{}, newError("%s: %s", fn, err)
	}
	lit, err := runtime.Literal(kind, abs)
	if err != nil {
		return fsTarget{}, newError("%s: %s", fn, err)
	}
	want, err := t.Admit(lit)
	var denied *runtime.DeniedError
	if errors.As(err, &denied) {
		return fsTarget{}, newError("%s", err)
	}
	if err != nil { // resolution failed inside a grant: the OS error
		return fsTarget{}, newError("%s: %s", fn, err)
	}
	g, ok := t.Caps.Grant(want)
	if !ok { // Admit just found a cover, and grants never change
		return fsTarget{}, newError("%s", t.Require(want))
	}
	return fsTarget{outer: confiningRoot(t.LineageCaps(), g.Scope), grant: g.Scope, path: want.Scope}, nil
}

func oneString(fn string, args []value.Object) (string, *value.Error) {
	if len(args) != 1 {
		return "", newError("%s: want 1 argument, got %d", fn, len(args))
	}
	s, ok := args[0].(*value.String)
	if !ok {
		return "", newError("%s: argument must be STRING, got %s", fn, args[0].Type())
	}
	return s.Value, nil
}

// maxSleepMS is the longest sleep whose Duration (ns) fits in an int64.
const maxSleepMS = math.MaxInt64 / int64(time.Millisecond)

// sleep(ms) pauses the task. It needs no capability, but it is an effect:
// it observes time, and it returns early with an error if cancelled.
func sleep(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 1 {
		return newError("sleep: want 1 argument, got %d", len(args))
	}
	ms, ok := args[0].(*value.Integer)
	if !ok || ms.Value < 0 {
		return newError("sleep: want a non-negative INTEGER of milliseconds")
	}
	if ms.Value > maxSleepMS {
		// Converting to a Duration would wrap around and sleep briefly.
		return newError("sleep: %d ms is too long (max %d)", ms.Value, int64(maxSleepMS))
	}
	timer := time.NewTimer(time.Duration(ms.Value) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return NIL
	case <-t.Ctx.Done():
		return newError("sleep cancelled: %s", context.Cause(t.Ctx))
	}
}
