package evaluator

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxBody caps how much of an HTTP response or file an effect will read.
const maxBody = 10 << 20

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
}

// capabilityConstructor builds net("host"), fs_read("path"), ... values.
// Relative paths resolve against the working directory.
func capabilityConstructor(kind runtime.Kind) value.BuiltinFunction {
	return func(t *runtime.Task, args ...value.Object) value.Object {
		scope, errObj := oneString(string(kind), args)
		if errObj != nil {
			return errObj
		}
		if kind == runtime.FSRead || kind == runtime.FSWrite {
			abs, err := filepath.Abs(scope)
			if err != nil {
				return newError("%s: %s", kind, err)
			}
			scope = abs
		}
		c, err := runtime.New(kind, scope)
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
	want, _ := runtime.New(runtime.Net, u.Hostname())
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
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
	f, err := os.OpenFile(path, os.O_RDONLY|oNoFollow, 0)
	if err != nil {
		return newError("read_file: %s", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxBody))
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
	abs, err := filepath.Abs(raw)
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
