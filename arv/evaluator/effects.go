package evaluator

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
	"github.com/delavalom/arvlang/arv/value"
)

// maxBody caps how much of an HTTP response or file an effect will read.
// A larger body is an error, never silently truncated.
const maxBody = 10 << 20

var errTooBig = fmt.Errorf("body exceeds %d MiB limit", maxBody>>20)

// readChunk is how much readCapped reads between two charges.
const readChunk = 32 << 10

// readCapped reads all of r, failing with errTooBig past maxBody bytes. It
// reads one byte more than the cap to tell "exactly maxBody" from "more".
// It charges each read to charge (alloc_mb; nil charges nothing) before
// keeping it, and stops with charge's error, so a reader holds at most one
// uncharged chunk: readers running at once under one scope stop together
// near its limit, not each at maxBody. It reads into fixed chunks and
// copies them once into a string of exactly the result's size, so the
// result keeps no slack alive (an Arv string holds its whole buffer, and
// slack is never charged), and a read allocates about twice its result.
// hint, unless negative, is the expected size (a file's, or a response's
// Content-Length): the first chunk is hint+1 bytes if that is smaller, so
// a small read does not allocate a whole chunk it never charges.
func readCapped(r io.Reader, hint int64, charge func(int64) error) (string, error) {
	r = io.LimitReader(r, maxBody+1)
	var chunks [][]byte
	total := 0
	for {
		if len(chunks) == 0 || len(chunks[len(chunks)-1]) == cap(chunks[len(chunks)-1]) {
			size := readChunk
			if len(chunks) == 0 && hint >= 0 && hint < readChunk {
				size = int(hint) + 1 // one more byte reads the end
			}
			chunks = append(chunks, make([]byte, 0, size))
		}
		c := chunks[len(chunks)-1]
		n, err := r.Read(c[len(c):cap(c)])
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
// itself, like every budget error, anything else prefixed with what, of
// kind effect (or cancelled, if the read was).
func readError(what string, err error) *value.Error {
	var exceeded *runtime.ExceededError
	if errors.As(err, &exceeded) {
		return errorOf(err, runtime.KindBudget)
	}
	return newErrorKind(runtime.ErrorKind(err, runtime.KindEffect), "%s: %s", what, err)
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

func init() {
	for name, kind := range map[string]runtime.Kind{
		"net": runtime.Net, "fs_read": runtime.FSRead, "fs_write": runtime.FSWrite, "env": runtime.Env,
	} {
		builtins[name] = &value.Builtin{Fn: capabilityConstructor(kind)}
	}
	builtins["http_get"] = &value.Builtin{Charging: httpGet}
	builtins["read_file"] = &value.Builtin{Charging: readFile}
	builtins["write_file"] = &value.Builtin{Charging: writeFile}
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

// readFile charges the file's bytes as it reads them, as a
// ChargingFunction.
func readFile(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	tg, errObj := fsRequest(t, charge, "read_file", runtime.FSRead, args)
	if errObj != nil {
		return errObj
	}
	f, err := openRegularIn(tg)
	if err != nil {
		return newErrorKind(runtime.KindEffect, "read_file: %s", err)
	}
	defer f.Close()
	hint := int64(-1)
	if fi, err := f.Stat(); err == nil {
		hint = fi.Size()
	}
	data, err := readCapped(f, hint, charge)
	if err != nil {
		return readError("read_file", err)
	}
	return &value.String{Value: data}
}

// writeFile charges only what fsRequest charges, as a ChargingFunction:
// it returns nil.
func writeFile(t *runtime.Task, charge func(int64) error, args ...value.Object) value.Object {
	if len(args) != 2 {
		return newError("write_file: want 2 arguments (path, text), got %d", len(args))
	}
	text, ok := args[1].(*value.String)
	if !ok {
		return newError("write_file: text must be STRING, got %s", args[1].Type())
	}
	tg, errObj := fsRequest(t, charge, "write_file", runtime.FSWrite, args[:1])
	if errObj != nil {
		return errObj
	}
	if err := writeIn(tg, text.Value); err != nil {
		return newErrorKind(runtime.KindEffect, "write_file: %s", err)
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
		return errorOf(err, runtime.KindCapability)
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
// the task and of every ancestor, Task.Lineage, charging each stat to
// charge): the caller must open that path beneath that grant (openIn), so
// the check and the use agree (I3) even if the tree changes in between.
func fsRequest(t *runtime.Task, charge func(int64) error, fn string, kind runtime.Kind, args []value.Object) (fsTarget, *value.Error) {
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
	// Canonicalising lstats every prefix of the path, O(depth x length)
	// bytes: charged, and a failed charge is the budget's error, bare.
	var spent error
	ch := charge
	if charge != nil {
		ch = func(n int64) error { spent = charge(n); return spent }
	}
	want, err := t.Admit(lit, ch)
	if spent != nil {
		return fsTarget{}, errorOf(spent, runtime.KindBudget)
	}
	var denied *runtime.DeniedError
	if errors.As(err, &denied) {
		return fsTarget{}, errorOf(err, runtime.KindCapability)
	}
	if err != nil { // resolution failed inside a grant: the OS error
		return fsTarget{}, newErrorKind(runtime.KindEffect, "%s: %s", fn, err)
	}
	g, ok := t.Caps.Grant(want)
	if !ok { // Admit just found a cover, and grants never change
		return fsTarget{}, errorOf(t.Require(want), runtime.KindCapability)
	}
	outer, err := confiningRoot(t.Lineage(), g.Scope, charge)
	if err != nil { // only charge fails it: the budget's error, bare
		return fsTarget{}, errorOf(err, runtime.KindBudget)
	}
	return fsTarget{outer: outer, grant: g.Scope, path: want.Scope}, nil
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
		return newErrorKind(runtime.KindCancelled, "sleep cancelled: %s", context.Cause(t.Ctx))
	}
}
