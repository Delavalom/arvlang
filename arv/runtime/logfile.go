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
// between is one effect or one check of one task, or a task's end:
//
//   - an effect: Task, Seq (1, 2, ... per task), Kind (the builtin's
//     name, or "spawn"), Req (its arguments, value.EncodeRequest), Res
//     (its result, value.Encode), Charged (the bytes it charged to
//     alloc_mb), Cancelled (its result is a cancellation) and, for puts,
//     Out (the index of each line it printed in the run's whole output);
//   - a check: Task, Seq, Kind ("cancelled", "exceeded" or "expired")
//     and Check, the ordinal of the task's check that saw it (see
//     Task.Err). A check that saw nothing is not logged;
//   - the end of a spawned task: Task, Seq and Kind KindDone, its last
//     record (Task.Finished).
//
// Every record also has At, the run's milliseconds when it was logged,
// and an effect Began, its milliseconds when it started: a resumed run's
// deadlines count the time the crashed run had spent. A cancelled check,
// or an effect whose result is a cancellation, has By if a deadline
// caused it, and an "expired" check always: the timed scope's ID
// (Task.OpenTimed).
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
	Began     int64           `json:"began,omitempty"`
	At        int64           `json:"at,omitempty"`
	By        string          `json:"by,omitempty"`
	End       *End            `json:"end,omitempty"`
	H         string          `json:"h,omitempty"`

	line int // ReadLog: the record's place among the log's records, from 0
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

// KindDone is the kind of the record that ends a spawned task.
const KindDone = "done"

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
	// sighted is, for each timed scope a record names (By), the line of
	// the first record that does: where its deadline fired in the log.
	sighted map[string]int
	outs    []uint64 // every Out index, sorted
	last    string   // the last whole line's H
	n       int      // records, header and End excluded
	at      int64    // the largest At: the crashed run had run at least this long
}

// Records is how many effect and check records the log holds.
func (l *Log) Records() int { return l.n }

// ReadLog reads and verifies an effect log: the chain, the header first
// and End last, each task's Seq numbers 1, 2, 3, ... with none missing,
// and no output index used twice. It never panics on any input.
func ReadLog(r io.Reader) (*Log, error) {
	l := &Log{tasks: map[string][]LogRecord{}, sighted: map[string]int{}}
	seen := map[uint64]bool{}
	br := bufio.NewReader(r)
	for lineNo := 1; ; lineNo++ {
		line, err := br.ReadBytes('\n')
		if err == io.EOF {
			if len(line) > 0 && l.End != nil {
				return nil, fmt.Errorf("effect log line %d: bytes after the end of the run", lineNo)
			}
			l.Truncated = len(line) > 0
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
	if rec.Began < 0 || rec.At < rec.Began || rec.Check != 0 && rec.Began != 0 {
		return fmt.Errorf("task %s: a record's times are out of order", rec.Task)
	}
	done := rec.Kind == KindDone && rec.Check == 0
	if done && (rec.Req != nil || rec.Res != nil || rec.Out != nil || rec.Charged != 0 || rec.Cancelled || rec.Began != 0) {
		return fmt.Errorf("task %s: a task's end holds only its time", rec.Task)
	}
	if rec.By != "" && !rec.Cancelled && rec.Kind != CheckCancelled && rec.Kind != CheckExpired {
		return fmt.Errorf("task %s: only a cancellation or a timeout names a deadline", rec.Task)
	}
	l.at = max(l.at, rec.At)
	recs := l.tasks[rec.Task]
	if rec.Seq != uint64(len(recs))+1 {
		return fmt.Errorf("task %s: record %d where %d comes next", rec.Task, rec.Seq, len(recs)+1)
	}
	if n := len(recs); n > 0 && recs[n-1].Kind == KindDone && recs[n-1].Check == 0 {
		return fmt.Errorf("task %s: a record after its end", rec.Task)
	}
	for _, k := range rec.Out {
		if seen[k] {
			return fmt.Errorf("output line %d printed twice", k)
		}
		seen[k] = true
		l.outs = append(l.outs, k)
	}
	rec.line = l.n
	if _, ok := l.sighted[rec.By]; rec.By != "" && !ok {
		l.sighted[rec.By] = rec.line
	}
	l.tasks[rec.Task] = append(recs, *rec)
	l.n++
	return nil
}
