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
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxBody caps how much of an HTTP response or file an effect will read.
// A larger body is an error, never silently truncated.
const maxBody = 10 << 20

var errTooBig = fmt.Errorf("body exceeds %d MiB limit", maxBody>>20)

// readCapped reads all of r, failing with errTooBig past maxBody bytes. It
// reads one byte more than the cap to tell "exactly maxBody" from "more".
func readCapped(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBody {
		return nil, errTooBig
	}
	return data, nil
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
// on every redirect, so a granted host cannot bounce the task elsewhere.
func clientFor(t *runtime.Task) *http.Client {
	return &http.Client{
		Timeout: 30 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return errors.New("too many redirects")
			}
			want, err := runtime.New(runtime.Net, req.URL.Hostname())
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
	builtins["http_get"] = &value.Builtin{Fn: httpGet}
	builtins["read_file"] = &value.Builtin{Fn: readFile}
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

func httpGet(t *runtime.Task, args ...value.Object) value.Object {
	raw, errObj := oneString("http_get", args)
	if errObj != nil {
		return errObj
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return newError("http_get: invalid url %q", raw)
	}
	if u.Hostname() == "" {
		return newError("http_get: URL has no host")
	}
	want, err := runtime.New(runtime.Net, u.Hostname())
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
	resp, err := clientFor(t).Do(req)
	if err != nil {
		var denied *runtime.DeniedError
		if errors.As(err, &denied) {
			return newError("%s", denied)
		}
		return newError("http_get: %s", err)
	}
	defer resp.Body.Close()
	body, err := readCapped(resp.Body)
	if errors.Is(err, errTooBig) {
		return newError("http_get: %s", err)
	}
	if err != nil {
		return newError("http_get: reading body: %s", err)
	}
	return value.NewHash(map[string]value.Object{
		"status": &value.Integer{Value: int64(resp.StatusCode)},
		"body":   &value.String{Value: string(body)},
	})
}

func readFile(t *runtime.Task, args ...value.Object) value.Object {
	path, errObj := fsRequest(t, "read_file", runtime.FSRead, args)
	if errObj != nil {
		return errObj
	}
	f, err := openRegular(path)
	if err != nil {
		return newError("read_file: %s", err)
	}
	defer f.Close()
	data, err := readCapped(f)
	if err != nil {
		return newError("read_file: %s", err)
	}
	return &value.String{Value: string(data)}
}

func writeFile(t *runtime.Task, args ...value.Object) value.Object {
	if len(args) != 2 {
		return newError("write_file: want 2 arguments (path, text), got %d", len(args))
	}
	text, ok := args[1].(*value.String)
	if !ok {
		return newError("write_file: text must be STRING, got %s", args[1].Type())
	}
	path, errObj := fsRequest(t, "write_file", runtime.FSWrite, args[:1])
	if errObj != nil {
		return errObj
	}
	if err := writeNoFollow(path, []byte(text.Value)); err != nil {
		return newError("write_file: %s", err)
	}
	return NIL
}

// writeNoFollow is os.WriteFile(path, data, 0o644) except that it refuses to
// follow a symlink at the final component. path is canonical (fsRequest), so
// it has no symlinks when checked; O_NOFOLLOW stops one planted at the leaf
// between that check and this open. A directory component swapped for a
// symlink in that window is still followed: see runtime.Canonical.
func writeNoFollow(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|oNoFollow, 0o644)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
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
func fsRequest(t *runtime.Task, fn string, kind runtime.Kind, args []value.Object) (string, *value.Error) {
	raw, errObj := oneString(fn, args)
	if errObj != nil {
		return "", errObj
	}
	if raw == "" {
		return "", newError("%s: empty path", fn)
	}
	abs, err := absPath(raw)
	if err != nil {
		return "", newError("%s: %s", fn, err)
	}
	// New canonicalises: every symlink, dangling or not, is resolved, so the
	// check below is on the path the OS will reach. The caller must open
	// want.Scope (not raw) so the check and the use agree (I3).
	want, err := runtime.New(kind, abs)
	if err != nil {
		return "", newError("%s: %s", fn, err)
	}
	if err := t.Require(want); err != nil {
		return "", newError("%s", err)
	}
	return want.Scope, nil
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
