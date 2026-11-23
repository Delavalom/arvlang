package check

import (
	"errors"

	"github.com/delavalom/arvlang/arv/ast"
)

// errNoURL is why http(req) is never allowed when req has no url key.
var errNoURL = errors.New("the request has no url")

// requestURL is the url of http(req) when req is a hash literal, or a name
// bound once to one, whose keys are all known strings, exactly one of them
// "url", bound to a known string. ok is false when it is not known. A
// request whose keys are all known and none is url is errNoURL: http
// refuses it before requiring anything.
func (a *analysis) requestURL(e ast.Expression) (url string, ok bool, err error) {
	if id, isID := e.(*ast.Identifier); isID {
		e = a.consts.lets[id.Value] // nil if not bound exactly once
	}
	h, isHash := e.(*ast.HashLiteral)
	if !isHash {
		return "", false, nil
	}
	var val ast.Expression
	for _, k := range h.Keys {
		name, known := a.constString(k)
		if !known {
			return "", false, nil
		}
		if name == "url" {
			if val != nil {
				return "", false, nil
			}
			val = h.Pairs[k]
		}
	}
	if val == nil {
		return "", false, errNoURL
	}
	url, ok = a.constString(val)
	return url, ok, nil
}
