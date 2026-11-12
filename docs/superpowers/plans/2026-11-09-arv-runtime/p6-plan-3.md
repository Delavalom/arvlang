# Phase 6, task p6.3: The log file: records, the hash chain, ReadLog

> **For agentic workers:** this file is one task, p6.3. Do it test first, exactly as written; if something here does not match the code you find, stop and report instead of improvising. Prerequisite: tasks p6.1 to p6.2 are committed in your tree. Start every shell command with `cd <your worktree> &&`. Before committing: `gofmt -l .` prints nothing, `go vet ./...` is clean and `go test -race ./...` is green. End the commit message with a blank line and then `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never use `--no-verify`; keep Go files under 15 KB and markdown under 18 KB. `go.mod` says `go 1.24.0` (toolchain 1.26.2), so do not use `testing/synctest` or `sync.WaitGroup.Go`. `arv/runtime` stays stdlib-only.
>
> Every code block below was applied, in task order, to a scratch copy of the tree and passed `go vet ./...` and `go test -race ./...` (including `e2e/`). Design: `13-phase6-durable-replay.md`. Deviations from it, and why: `p6-plan-1.md`.

### Task p6.3: The log file: records, the hash chain, ReadLog

`arv/runtime/logfile.go` defines the log's lines and reads them back; the tests build logs with `logWriter` (a run writes one from p6.4 on).
- `LogRecord` is one JSON line: the header, an effect, a check or the `end` (see its doc). `seq` counts 1, 2, ... per task over effects and checks.
- `logWriter.append` writes each line in one `Write`, then `sync` (fsync) if set, and ends it with `"h"`: SHA-256 of the previous line's `h` and this line without `h`.
- `ReadLog` verifies the chain, a version-1 header first, nothing after the end, contiguous `seq` per task, check records, and unique output indices. A partial last line is dropped (`Truncated`); `Size` is where an append starts. It never panics (fuzzed).

**Files:**
- Create: `arv/runtime/logfile.go`
- Create (test): `arv/runtime/logfile_test.go`

- [ ] **Step 1: Write the failing tests**

Create `arv/runtime/logfile_test.go`:

```go
package runtime

import (
	"bytes"
	"strings"
	"testing"
)

// sampleLog is the log of a small run: root makes two effects, root/0
// one, root prints a line, and the run ends with result.
func sampleLog(t testing.TB, result string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := &logWriter{w: &buf}
	h := Header{ScriptSHA256: "abc", Grants: []LogGrant{{Kind: Net, Scope: "127.0.0.1"}}, Limits: LogLimits{Steps: 9}}
	for _, r := range []*LogRecord{
		{V: LogVersion, Header: &h},
		{Task: "root", Seq: 1, Kind: "getenv", Req: []byte(`["a"]`), Res: []byte(`"v"`), Charged: 1},
		{Task: "root", Seq: 2, Kind: "getenv", Req: []byte(`["b"]`), Res: []byte(`"v"`), Charged: 1},
		{Task: "root/0", Seq: 1, Kind: "sleep", Req: []byte(`[5]`), Res: []byte(`null`)},
		{Task: "root", Seq: 3, Kind: "puts", Req: []byte(`["hi"]`), Out: []uint64{0}},
		{End: &End{Result: []byte(result)}},
	} {
		if err := w.append(r); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

func TestLogRoundTrip(t *testing.T) {
	data := sampleLog(t, `[7]`)
	l, err := ReadLog(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("ReadLog: %v\n%s", err, data)
	}
	if l.Header.ScriptSHA256 != "abc" || len(l.Header.Grants) != 1 || l.Header.Limits.Steps != 9 {
		t.Errorf("header = %+v", l.Header)
	}
	if l.End == nil || string(l.End.Result) != `[7]` || l.Truncated || l.Size != int64(len(data)) || l.Records() != 4 {
		t.Errorf("end = %+v, truncated %v, size %d of %d, records %d", l.End, l.Truncated, l.Size, len(data), l.Records())
	}
	if got := l.tasks["root"]; len(got) != 3 || got[1].Kind != "getenv" || string(got[1].Req) != `["b"]` || got[1].Charged != 1 || len(got[2].Out) != 1 {
		t.Errorf("root's records = %+v", got)
	}
	if !strings.HasPrefix(string(data), `{"v":1,"header":{"script_sha256":"abc","grants":[{"kind":"net","scope":"127.0.0.1"}]`) {
		t.Errorf("first line = %.120s", data)
	}
}

// A log that was edited, cut, reordered or extended is refused with a
// clean error, never a panic (I7).
func TestReadLogRefusesTampering(t *testing.T) {
	data := sampleLog(t, `[7]`)
	lines := strings.SplitAfter(string(data), "\n")
	lines = lines[:len(lines)-1] // the empty string after the last newline
	join := func(ls ...string) string { return strings.Join(ls, "") }
	for _, tt := range []struct{ name, log, want string }{
		{"empty", "", "empty"},
		{"edited result", strings.Replace(string(data), `"v"`, `"w"`, 1), "hash chain broken"},
		{"edited header", strings.Replace(string(data), `127.0.0.1`, `10.0.0.1`, 1), "hash chain broken"},
		{"dropped record", join(lines[0], lines[2], lines[3], lines[4], lines[5]), "hash chain broken"},
		{"swapped records", join(lines[0], lines[2], lines[1], lines[3], lines[4], lines[5]), "hash chain broken"},
		{"no header", join(lines[1:]...), "hash chain broken"},
		{"line after the end", string(data) + lines[1], "hash chain broken"},
		{"not a log", "hello\n", "does not end with its hash"},
		{"bad hash text", strings.Replace(lines[0], `"h":"`, `"h":"x`, 1), "does not end with its hash"},
	} {
		if _, err := ReadLog(strings.NewReader(tt.log)); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// A run killed mid-line leaves a partial last line: it is dropped and
// reported, and Size says where an append must start.
func TestReadLogTruncated(t *testing.T) {
	data := sampleLog(t, `[7]`)
	cut := bytes.LastIndexByte(data[:len(data)-1], '\n') + 1 // the start of the End line
	l, err := ReadLog(bytes.NewReader(data[:cut+10]))
	if err != nil {
		t.Fatal(err)
	}
	if !l.Truncated || l.End != nil || l.Size != int64(cut) || l.Records() != 4 {
		t.Fatalf("truncated %v, end %v, size %d (want %d), records %d", l.Truncated, l.End, l.Size, cut, l.Records())
	}
}

// Lines that chain correctly can still be wrong: whoever can write the
// file can recompute the chain. ReadLog checks what each line may hold.
func TestReadLogChecksStructure(t *testing.T) {
	head := &LogRecord{V: LogVersion, Header: &Header{}}
	for _, tt := range []struct {
		name string
		recs []*LogRecord
		want string
	}{
		{"seq gap", []*LogRecord{head, {Task: "root", Seq: 2, Kind: "sleep"}}, "record 2 where 1 comes next"},
		{"seq repeat", []*LogRecord{head, {Task: "root", Seq: 1, Kind: "sleep"}, {Task: "root", Seq: 1, Kind: "sleep"}}, "record 1 where 2"},
		{"no kind", []*LogRecord{head, {Task: "root", Seq: 1}}, "task and a kind"},
		{"second header", []*LogRecord{head, head}, "header"},
		{"wrong version", []*LogRecord{{V: 2, Header: &Header{}}}, "version 1 header"},
		{"record first", []*LogRecord{{Task: "root", Seq: 1, Kind: "x"}}, "header first"},
		{"bad check", []*LogRecord{head, {Task: "root", Seq: 1, Kind: "sleep", Check: 3}}, "check record"},
		{"output twice", []*LogRecord{head, {Task: "root", Seq: 1, Kind: "puts", Out: []uint64{0}}, {Task: "root/0", Seq: 1, Kind: "puts", Out: []uint64{0}}}, "printed twice"},
		{"line after the end", []*LogRecord{head, {End: &End{Result: []byte(`[1]`)}}, {Task: "root", Seq: 1, Kind: "x"}}, "after the end"},
		{"end with more", []*LogRecord{head, {Task: "root", Kind: "x", End: &End{Result: []byte(`[1]`)}}}, "only the run's result"},
	} {
		var buf bytes.Buffer
		w := &logWriter{w: &buf}
		for _, r := range tt.recs {
			if err := w.append(r); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := ReadLog(&buf); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: err = %v, want %q", tt.name, err, tt.want)
		}
	}
}

// FuzzReadLog: ReadLog never panics, and whatever it accepts it accepts
// again from its own Size bytes.
func FuzzReadLog(f *testing.F) {
	f.Add(sampleLog(f, `[7]`))
	f.Add([]byte("{}\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		l, err := ReadLog(bytes.NewReader(data))
		if err != nil {
			return
		}
		if _, err := ReadLog(bytes.NewReader(data[:l.Size])); err != nil {
			t.Fatalf("accepted %q but not its whole lines: %v", data, err)
		}
	})
}
```

- [ ] **Step 2: Run them and watch them fail**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestLogRoundTrip|TestReadLog'`

Expected: the package fails to build: `undefined: logWriter`, `undefined: LogRecord`, `undefined: ReadLog`, ...

- [ ] **Step 3: Implement**

Create `arv/runtime/logfile.go`:

```go
package runtime

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
)

// LogVersion is the effect log format this runtime writes and reads.
const LogVersion = 1

// LogRecord is one line of an effect log (JSON Lines). The first line holds
// V and Header, the last of a finished run holds End, and every line in
// between is one effect or one check of one task:
//
//   - an effect: Task, Seq (1, 2, ... per task), Kind (the builtin's
//     name, or "spawn"), Req (its arguments, value.EncodeRequest), Res
//     (its result, value.Encode), Charged (the bytes it charged to
//     alloc_mb), Cancelled (its result is a cancellation) and, for puts,
//     Out (the index of each line it printed in the run's whole output);
//   - a check: Task, Seq, Kind ("cancelled", "exceeded" or "expired")
//     and Check, the ordinal of the task's check that saw it (see
//     Task.Err). A check that saw nothing is not logged.
//
// H chains the lines: the SHA-256, in hex, of the previous line's H and
// this line without H. Editing, dropping or reordering a line breaks the
// chain from there on. It is not a signature: whoever can write the file
// can recompute it.
type LogRecord struct {
	V         int             `json:"v,omitempty"`
	Header    *Header         `json:"header,omitempty"`
	Task      string          `json:"task,omitempty"`
	Seq       uint64          `json:"seq,omitempty"`
	Kind      string          `json:"kind,omitempty"`
	Check     uint64          `json:"check,omitempty"`
	Req       json.RawMessage `json:"req,omitempty"`
	Res       json.RawMessage `json:"res,omitempty"`
	Charged   int64           `json:"charged,omitempty"`
	Cancelled bool            `json:"cancelled,omitempty"`
	Out       []uint64        `json:"out,omitempty"`
	End       *End            `json:"end,omitempty"`
	H         string          `json:"h,omitempty"`
}

// Header describes the recorded run: what replay needs to run it again.
type Header struct {
	ScriptSHA256 string     `json:"script_sha256"`
	Grants       []LogGrant `json:"grants"`
	Limits       LogLimits  `json:"limits"`
	Cwd          string     `json:"cwd,omitempty"`
	Started      string     `json:"started,omitempty"`
}

// LogGrant is one capability the host granted the root task, canonical.
type LogGrant struct {
	Kind  Kind   `json:"kind"`
	Scope string `json:"scope"`
}

// LogLimits are the run's Limits.
type LogLimits struct {
	TimeoutMS  int64 `json:"timeout_ms,omitempty"`
	Steps      int64 `json:"steps,omitempty"`
	LiveTasks  int   `json:"live_tasks,omitempty"`
	AllocBytes int64 `json:"alloc_bytes,omitempty"`
}

// End closes the log of a run that finished: Result is the run's value
// (an error too), as value.EncodeRequest writes a one-element array.
type End struct {
	Result json.RawMessage `json:"result"`
}

// The outcomes a check logs (see Task.Err, Task.Acquire, Task.Expired).
const (
	CheckCancelled = "cancelled"
	CheckExceeded  = "exceeded"
	CheckExpired   = "expired"
)

var checkKinds = map[string]bool{CheckCancelled: true, CheckExceeded: true, CheckExpired: true}

// hashSuffix is what follows a line's body: `,"h":"<64 hex>"}`.
const hashSuffixLen = len(`,"h":""}`) + 2*sha256.Size

// chain is the H of a line whose body (the line without H) is body,
// after a line whose H was prev.
func chain(prev string, body []byte) string {
	h := sha256.New()
	h.Write([]byte(prev))
	h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}

// logWriter appends records to a log, one write per line, so a run killed
// between two records leaves only whole lines (or, if killed during the
// write, one partial last line, which ReadLog reports as Truncated).
type logWriter struct {
	mu   sync.Mutex
	w    io.Writer
	sync func() error // after each line, unless nil: fsync
	prev string       // the last line's H
	err  error        // the first failure; nothing is written after it
}

func (lw *logWriter) append(r *LogRecord) error {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false) // Req and Res stay byte for byte as encoded
	r.H = ""
	if err := enc.Encode(r); err != nil {
		return fmt.Errorf("effect log: %w", err)
	}
	body := bytes.TrimSuffix(buf.Bytes(), []byte("\n"))
	lw.mu.Lock()
	defer lw.mu.Unlock()
	if lw.err != nil {
		return lw.err
	}
	h := chain(lw.prev, body)
	line := make([]byte, 0, len(body)+hashSuffixLen+1)
	line = append(line, body[:len(body)-1]...)
	line = append(line, `,"h":"`...)
	line = append(line, h...)
	line = append(line, "\"}\n"...)
	if _, err := lw.w.Write(line); err != nil {
		lw.err = fmt.Errorf("effect log: %w", err)
		return lw.err
	}
	if lw.sync != nil {
		if err := lw.sync(); err != nil {
			lw.err = fmt.Errorf("effect log: fsync: %w", err)
			return lw.err
		}
	}
	lw.prev = h
	return nil
}

// Log is an effect log read back and verified (ReadLog).
type Log struct {
	Header Header
	// End is the closing line, nil if the run did not finish: it was
	// killed, or its host cancelled it.
	End *End
	// Truncated reports a partial last line, dropped: the run was killed
	// while writing it.
	Truncated bool
	// Size is the bytes of the whole lines: where an append must start.
	Size int64

	tasks map[string][]LogRecord // each task's records, in Seq order
	outs  []uint64               // every Out index, sorted
	last  string                 // the last whole line's H
	n     int                    // records, header and End excluded
}

// Records is how many effect and check records the log holds.
func (l *Log) Records() int { return l.n }

// ReadLog reads and verifies an effect log: the chain, the header first
// and End last, each task's Seq numbers 1, 2, 3, ... with none missing,
// and no output index used twice. It never panics on any input.
func ReadLog(r io.Reader) (*Log, error) {
	l := &Log{tasks: map[string][]LogRecord{}}
	seen := map[uint64]bool{}
	br := bufio.NewReader(r)
	for lineNo := 1; ; lineNo++ {
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			if len(line) > 0 {
				l.Truncated = true
			}
			break
		}
		if err != nil {
			return nil, fmt.Errorf("effect log: %w", err)
		}
		rec, err := l.verify(line)
		if err != nil {
			return nil, fmt.Errorf("effect log line %d: %w", lineNo, err)
		}
		if err := l.add(rec, lineNo, seen); err != nil {
			return nil, fmt.Errorf("effect log line %d: %w", lineNo, err)
		}
		l.Size += int64(len(line))
	}
	if l.Size == 0 {
		return nil, errors.New("effect log: empty, or no whole line")
	}
	slices.Sort(l.outs)
	return l, nil
}

// verify checks line's place in the chain and decodes it.
func (l *Log) verify(line []byte) (*LogRecord, error) {
	line = bytes.TrimSuffix(line, []byte("\n"))
	if len(line) < hashSuffixLen+2 || !bytes.HasPrefix(line[len(line)-hashSuffixLen:], []byte(`,"h":"`)) ||
		!bytes.HasSuffix(line, []byte(`"}`)) {
		return nil, errors.New("not a log line: it does not end with its hash")
	}
	h := string(line[len(line)-hashSuffixLen+len(`,"h":"`) : len(line)-2])
	body := append(line[:len(line)-hashSuffixLen:len(line)-hashSuffixLen], '}')
	if chain(l.last, body) != h {
		return nil, errors.New("hash chain broken: the log was edited, reordered or cut")
	}
	var rec LogRecord
	d := json.NewDecoder(bytes.NewReader(body))
	d.DisallowUnknownFields()
	if err := d.Decode(&rec); err != nil {
		return nil, err
	}
	if d.More() {
		return nil, errors.New("data after the record")
	}
	l.last = h
	return &rec, nil
}

// add files rec, the lineNo-th line, after checking where it may be.
func (l *Log) add(rec *LogRecord, lineNo int, seen map[uint64]bool) error {
	switch {
	case l.End != nil:
		return errors.New("a line after the end of the run")
	case lineNo == 1:
		if rec.V != LogVersion || rec.Header == nil || rec.Task != "" || rec.End != nil {
			return fmt.Errorf("want a version %d header first", LogVersion)
		}
		l.Header = *rec.Header
		if l.Header.Grants == nil {
			l.Header.Grants = []LogGrant{}
		}
		return nil
	case rec.End != nil:
		if rec.V != 0 || rec.Header != nil || rec.Task != "" || rec.Kind != "" || rec.End.Result == nil {
			return errors.New("an end line holds only the run's result")
		}
		l.End = rec.End
		return nil
	case rec.V != 0 || rec.Header != nil:
		return errors.New("a second header")
	case rec.Task == "" || rec.Kind == "":
		return errors.New("a record needs a task and a kind")
	}
	if rec.Check != 0 && (!checkKinds[rec.Kind] || rec.Req != nil || rec.Res != nil || rec.Out != nil) {
		return fmt.Errorf("task %s: a check record holds only its kind (%s is not one) and its ordinal", rec.Task, rec.Kind)
	}
	recs := l.tasks[rec.Task]
	if rec.Seq != uint64(len(recs))+1 {
		return fmt.Errorf("task %s: record %d where %d comes next", rec.Task, rec.Seq, len(recs)+1)
	}
	for _, k := range rec.Out {
		if seen[k] {
			return fmt.Errorf("output line %d printed twice", k)
		}
		seen[k] = true
		l.outs = append(l.outs, k)
	}
	l.tasks[rec.Task] = append(recs, *rec)
	l.n++
	return nil
}
```

- [ ] **Step 4: Run the tests, then the full checks**

Run: `cd <worktree> && go test -race ./arv/runtime/ -run 'TestLogRoundTrip|TestReadLog'`, then `cd <worktree> && gofmt -l . && go vet ./... && go test -race ./...`

Expected: `ok` for every package, and `gofmt -l .` prints nothing. Then (check `df -h /System/Volumes/Data` first; stop if under 1 GB free): `cd <worktree> && go test ./arv/runtime/ -run '^$' -fuzz FuzzReadLog -fuzztime 20s` passes.

- [ ] **Step 5: Commit**

```bash
cd <worktree> && git add arv/runtime/logfile.go arv/runtime/logfile_test.go && git commit -F - <<'MSG'
feat(runtime): effect log file format with a hash chain

Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>
MSG
```
