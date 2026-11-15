package llm

import (
	"bytes"
	"encoding/json"
	"strings"
)

// holdsKey reports whether the reply body data holds key: as bytes, in
// any decoded JSON string or object key (an escape hides it from a byte
// search), or split across blocks (review round 2). ask returns the text
// blocks joined and a tool gets its arguments, so the text blocks joined,
// and every text and argument value in reply order, are checked as one
// string each. A key spread across turns, reordered or encoded is not
// caught (a known limit): the endpoint is trusted with the key anyway.
func holdsKey(data []byte, key string) bool {
	if bytes.Contains(data, []byte(key)) {
		return true
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return false // decodeResponse refuses it
	}
	if anyStringHolds(v, key) {
		return true
	}
	var r struct {
		Content []apiBlock `json:"content"`
	}
	if json.Unmarshal(data, &r) != nil {
		return false // decodeResponse refuses it
	}
	var text, all strings.Builder
	for _, b := range r.Content {
		switch b.Type {
		case BlockText:
			if b.Text != nil {
				text.WriteString(*b.Text)
				all.WriteString(*b.Text)
			}
		case BlockToolUse:
			appendValues(&all, b.Input)
		}
	}
	return strings.Contains(text.String(), key) || strings.Contains(all.String(), key)
}

// anyStringHolds reports whether any string or object key in v holds key.
func anyStringHolds(v any, key string) bool {
	switch v := v.(type) {
	case string:
		return strings.Contains(v, key)
	case []any:
		for _, e := range v {
			if anyStringHolds(e, key) {
				return true
			}
		}
	case map[string]any:
		for k, e := range v {
			if strings.Contains(k, key) || anyStringHolds(e, key) {
				return true
			}
		}
	}
	return false
}

// appendValues appends the string values in the JSON raw, in document
// order, to b; object keys are skipped.
func appendValues(b *strings.Builder, raw json.RawMessage) {
	type frame struct{ obj, key bool }
	var stack []frame
	advance := func() {
		if n := len(stack); n > 0 && stack[n-1].obj {
			stack[n-1].key = !stack[n-1].key
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{', '[':
				advance()
				stack = append(stack, frame{obj: t == '{', key: true})
			default:
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		default:
			isKey := len(stack) > 0 && stack[len(stack)-1].obj && stack[len(stack)-1].key
			advance()
			if s, ok := t.(string); ok && !isKey {
				b.WriteString(s)
			}
		}
	}
}
