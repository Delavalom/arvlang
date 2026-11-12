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

// Bytes after a finished run's end line are not a crash's partial line
// (the end line is the last thing written): the file was changed.
func TestReadLogRefusesBytesAfterTheEnd(t *testing.T) {
	data := sampleLog(t, `[7]`)
	if _, err := ReadLog(bytes.NewReader(append(data, "garbage"...))); err == nil || !strings.Contains(err.Error(), "after the end") {
		t.Fatalf("ReadLog = %v", err)
	}
	cut := data[:bytes.LastIndexByte(data[:len(data)-1], '\n')+1]
	if l, err := ReadLog(bytes.NewReader(append(cut, `{"end":`...))); err != nil || !l.Truncated || l.End != nil {
		t.Fatalf("a partial end line: %v", err)
	}
}
