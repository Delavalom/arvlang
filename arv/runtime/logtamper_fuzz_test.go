package runtime

import (
	"bytes"
	"testing"
)

// FuzzReadLogTamper (I7: a tampered or truncated log is detected): a
// recorded log, edited anywhere (bytes changed, inserted or deleted, the
// file cut, lines swapped), is either refused or read as a prefix of
// whole lines of the original, the shape a crash leaves. Nothing the
// edit changed is ever read as a record.
func FuzzReadLogTamper(f *testing.F) {
	orig := sampleLog(f, `[{"error":["boom","user","1:1","root"]}]`)
	f.Add(uint16(10), byte('x'), uint8(0), uint16(0))
	f.Add(uint16(len(orig)-1), byte(0), uint8(2), uint16(0))
	f.Add(uint16(len(orig)-30), byte(0), uint8(3), uint16(5))
	f.Add(uint16(0), byte(0), uint8(4), uint16(2))
	f.Add(uint16(200), byte('\n'), uint8(1), uint16(0))
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
			end := min(len(data), p+1+int(n)%64)
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
	})
}
