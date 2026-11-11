package runtime

import (
	"strconv"
	"strings"
)

// deniedPrefix starts every *DeniedError's message.
const deniedPrefix = "capability denied: "

// Redacted is the denial as a model may see it: what was asked for, and
// nothing about the task's other grants, its ID or a dial-time note. A
// net denial names no host or address at all: a dial-time denial's Want
// is the internal address a granted name resolved to, and naming the host
// only for the other kind would tell the model which names resolve
// inside. The model knows what it asked for.
func (e *DeniedError) Redacted() string {
	if e.Want.Kind == Net {
		return deniedPrefix + string(Net)
	}
	return deniedPrefix + e.Want.String()
}

// RedactDenials returns msg with its first capability denial, and
// everything after it, replaced by the denial's redacted form (see
// DeniedError.Redacted). A denial lists the grants the task holds (up to
// 8, maxHeldShown), and a dial-time one names the internal address a
// granted name resolved to; neither may reach a model (Phase 7). The text
// before the denial is kept. Everything after it is dropped: a quoted
// scope can hold any text, so where a held list ends cannot be told
// reliably, and a second denial there would carry its own list.
//
// It works on text, so it finds denials an Arv program passed on as
// strings (a tool that returns e["message"] from a match), not ones it
// disguised. If the request cannot be parsed back, the redacted form is
// just "capability denied".
func RedactDenials(msg string) string {
	i := strings.Index(msg, deniedPrefix)
	if i < 0 {
		return msg
	}
	keep := msg[:i]
	rest := msg[i+len(deniedPrefix):]
	open := strings.IndexByte(rest, '(')
	if open < 0 || !kinds[Kind(rest[:open])] {
		return keep + "capability denied"
	}
	kind := Kind(rest[:open])
	q, err := strconv.QuotedPrefix(rest[open+1:])
	if err != nil || !strings.HasPrefix(rest[open+1+len(q):], ")") {
		return keep + "capability denied"
	}
	scope, err := strconv.Unquote(q)
	if err != nil {
		return keep + "capability denied"
	}
	return keep + (&DeniedError{Want: Capability{Kind: kind, Scope: scope}}).Redacted()
}
