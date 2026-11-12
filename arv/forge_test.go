package arv

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// forge rewrites a log line by line, as someone who can write the file
// can: edit gets each line's body (the line without its hash, as JSON)
// and returns the new body, or nil to drop the line; the hash chain is
// then recomputed, so only the log's content can give the edit away.
func forge(t testing.TB, log []byte, edit func(i int, body []byte) []byte) []byte {
	t.Helper()
	var out bytes.Buffer
	prev := ""
	for i, line := range bytes.SplitAfter(log, []byte("\n")) {
		line = bytes.TrimSuffix(line, []byte("\n"))
		if len(line) == 0 {
			continue
		}
		body := line
		if k := bytes.LastIndex(line, []byte(`,"h":"`)); k >= 0 {
			body = append(append([]byte(nil), line[:k]...), '}')
		}
		if body = edit(i, body); body == nil {
			continue
		}
		sum := sha256.Sum256(append([]byte(prev), body...))
		prev = hex.EncodeToString(sum[:])
		out.Write(body[:len(body)-1])
		out.WriteString(`,"h":"` + prev + `"}` + "\n")
	}
	return out.Bytes()
}
