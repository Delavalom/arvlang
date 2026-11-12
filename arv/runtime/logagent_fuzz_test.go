package runtime

import (
	"bytes"
	"testing"
)

// agentLog is a log shaped like a recorded agent run (Phase 6x7): two
// tasks racing for one model budget, each logging llm_model, llm_reserve
// (one granted, one refused), llm (a whole request and reply), a tool's
// request, a host effect, a settlement past its reservation that failed,
// output, and the end. The records' bodies are cut-down copies of what
// the evaluator writes.
func agentLog(t testing.TB) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := &logWriter{w: &buf}
	h := Header{ScriptSHA256: "abc", Grants: []LogGrant{{Kind: LLM, Scope: "anthropic:*"}, {Kind: Net, Scope: "127.0.0.1"},
		{Kind: Tool, Scope: "fetch"}, {Kind: "k8s", Scope: "staging"}}, Limits: LogLimits{Steps: 1000000}}
	model := `{"hash":[["id","claude-haiku-5-5"],["input",100000],["output",500000],["priced",true]]}`
	call := `{"model":"anthropic:claude-haiku-5-5","request":{"Model":"claude-haiku-5-5","System":"","Messages":[{"Role":"user","Content":[{"Type":"text","Text":"main","ID":"","Name":"","Input":null,"IsError":false,"Raw":null}]}],"Tools":[{"Name":"fetch","Description":"","Params":["arg"]}],"MaxTokens":4096}}`
	reply := `{"resp":{"Content":[{"Type":"tool_use","Text":"","ID":"u0_0","Name":"fetch","Input":{"arg":"/a"},"IsError":false,"Raw":null}],"StopReason":"tool_use","InputTokens":6000,"OutputTokens":500}}`
	refused := `{"error":["budget exceeded: tokens (limit 10000)","budget","",""]}`
	for _, r := range []*LogRecord{
		{V: LogVersion, Header: &h},
		{Task: "root/0", Seq: 1, Kind: "llm_model", Req: []byte(`["agent","anthropic:claude-haiku-5-5"]`), Res: []byte(model)},
		{Task: "root/1", Seq: 1, Kind: "llm_model", Req: []byte(`["ask","anthropic:claude-haiku-5-5"]`), Res: []byte(model)},
		{Task: "root/0", Seq: 2, Kind: "llm_reserve", Req: []byte(`[4625,2101]`), Res: []byte(`null`)},
		{Task: "root/1", Seq: 2, Kind: "llm_reserve", Req: []byte(`[4625,2101]`), Res: []byte(refused)},
		{Task: "root/0", Seq: 3, Kind: "llm", Req: []byte(call), Res: []byte(reply), Began: 3, At: 9},
		{Task: "root/0", Seq: 4, Kind: "llm_settle", Req: []byte(`[6500,850]`), Res: []byte(refused)},
		{Task: "root/0", Seq: 5, Kind: "http_get", Req: []byte(`["http://127.0.0.1:1/a"]`), Res: []byte(`{"hash":[["body","body of /a"],["status",200]]}`), Charged: 1},
		{Task: "root/0", Seq: 6, Kind: "host:k8s_apply", Req: []byte(`["staging","web.yaml"]`), Res: []byte(`"applied to \"staging\""`)},
		{Task: "root/1", Seq: 3, Kind: "puts", Req: []byte(`["budget: budget exceeded: tokens (limit 10000)"]`), Out: []uint64{0}},
		{End: &End{Result: []byte(`["end"]`)}},
	} {
		if err := w.append(r); err != nil {
			t.Fatal(err)
		}
	}
	return buf.Bytes()
}

// FuzzReadLogAgentRecords (I7 for agent runs: a tampered or truncated log
// is detected): the agent-run log, edited anywhere (a byte changed,
// inserted or deleted, the file cut, two lines swapped), is either
// refused or read as a prefix of the original's whole lines, the shape a
// crash leaves; ReadLog never panics, and what it accepts it accepts
// again from its own Size bytes.
func FuzzReadLogAgentRecords(f *testing.F) {
	orig := agentLog(f)
	l, err := ReadLog(bytes.NewReader(orig))
	if err != nil || l.End == nil || l.Records() != 9 {
		f.Fatalf("the agent log: %v\n%s", err, orig)
	}
	for i, k := range [][]byte{[]byte(`"llm_reserve"`), []byte(`"llm_settle"`), []byte(`"InputTokens"`), []byte(`"llm_model"`), []byte(`"end"`)} {
		p := bytes.Index(orig, k)
		f.Add(uint16(p+1), byte('x'), uint8(i%4), uint16(i))
	}
	f.Add(uint16(0), byte(0), uint8(4), uint16(3))
	f.Add(uint16(len(orig)-1), byte('\n'), uint8(1), uint16(0))
	f.Fuzz(func(t *testing.T, pos uint16, b byte, op uint8, n uint16) {
		data := bytes.Clone(orig)
		p := int(pos) % (len(data) + 1)
		switch op % 5 {
		case 0: // change one byte
			if p == len(data) || data[p] == b {
				return
			}
			data[p] = b
		case 1: // insert one byte
			data = append(data[:p], append([]byte{b}, data[p:]...)...)
		case 2: // delete n+1 bytes
			end := min(len(data), p+1+int(n)%256)
			if p == end {
				return
			}
			data = append(data[:p], data[end:]...)
		case 3: // cut the file
			data = data[:p]
		case 4: // swap two lines
			lines := bytes.SplitAfter(data, []byte("\n"))
			i, j := p%len(lines), int(n)%len(lines)
			if bytes.Equal(lines[i], lines[j]) {
				return
			}
			lines[i], lines[j] = lines[j], lines[i]
			data = bytes.Join(lines, nil)
		}
		l, err := ReadLog(bytes.NewReader(data))
		if err != nil {
			return
		}
		whole := data[:l.Size]
		if !bytes.HasPrefix(orig, whole) || l.Size > 0 && orig[l.Size-1] != '\n' {
			t.Fatalf("ReadLog accepted an edit as whole lines:\n%s", whole)
		}
		if l.End != nil && !bytes.Equal(whole, orig) {
			t.Fatalf("ReadLog read an end line from a cut log:\n%s", data)
		}
		if l.Truncated != (len(data) > int(l.Size)) {
			t.Fatalf("Truncated = %v with %d bytes after %d whole", l.Truncated, len(data)-int(l.Size), l.Size)
		}
		again, err := ReadLog(bytes.NewReader(whole))
		if err != nil || again.Records() != l.Records() || again.Size != l.Size {
			t.Fatalf("accepted %q but not its whole lines alike: %v", data, err)
		}
	})
}
