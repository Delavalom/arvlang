package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// The Anthropic Messages API, as the claude-api skill described it on
// 2026-11-11: POST /v1/messages with x-api-key, anthropic-version and a
// JSON body; tool definitions carry a JSON Schema; replies are content
// blocks, sent back unchanged on the next turn.
const (
	AnthropicURL     = "https://api.anthropic.com/v1/messages"
	AnthropicVersion = "2026-10-21"
)

// maxResponse caps the bytes of one reply the provider reads. A reply is
// at most maxTokens tokens of output; a larger body is an error.
const maxResponse = 16 << 20

// Anthropic calls the Anthropic Messages API over plain net/http. The key
// stays in an unexported field: no method returns it, it is never put in
// an error, and printing the provider with any verb prints no key.
type Anthropic struct {
	key    string
	url    string
	client *http.Client
}

// NewAnthropic returns a provider that sends key to url (AnthropicURL if
// url is ""). Each call opens its own connection (keep-alives are off), so
// no connection, and no goroutine serving one, outlives the call. It never
// follows a redirect: net/http would re-send x-api-key to the new host.
func NewAnthropic(key, url string) *Anthropic {
	if url == "" {
		url = AnthropicURL
	}
	tr := &http.Transport{Proxy: http.ProxyFromEnvironment, DisableKeepAlives: true, TLSHandshakeTimeout: 30 * time.Second}
	noRedirect := func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Anthropic{key: key, url: url, client: &http.Client{Transport: tr, Timeout: 10 * time.Minute, CheckRedirect: noRedirect}}
}

// String, GoString and Format keep the key out of fmt's output, for every
// verb (%+v and %#v included), whether the provider is printed as a
// pointer or as a value: fmt would print an unexported field otherwise.
func (a Anthropic) String() string                { return "llm.Anthropic(" + a.url + ")" }
func (a Anthropic) GoString() string              { return a.String() }
func (a Anthropic) Format(f fmt.State, verb rune) { io.WriteString(f, a.String()) }

// Complete sends req and decodes the reply. Errors are *Error, except that
// once ctx has ended it returns ctx's cause. A reply that holds the key
// is a decode error (see holdsKey).
func (a *Anthropic) Complete(ctx context.Context, req Request) (Response, error) {
	body, err := encodeRequest(req)
	if err != nil {
		return Response{}, &Error{Provider: "anthropic", Kind: ErrInvalid, Message: err.Error()}
	}
	hreq, err := http.NewRequestWithContext(ctx, http.MethodPost, a.url, bytes.NewReader(body))
	if err != nil {
		return Response{}, a.fail(ErrInvalid, 0, err.Error())
	}
	hreq.Header.Set("x-api-key", a.key)
	hreq.Header.Set("anthropic-version", AnthropicVersion)
	hreq.Header.Set("content-type", "application/json")
	resp, err := a.client.Do(hreq)
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, context.Cause(ctx)
		}
		return Response{}, a.fail(ErrNetwork, 0, err.Error())
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		if ctx.Err() != nil {
			return Response{}, context.Cause(ctx)
		}
		return Response{}, a.fail(ErrNetwork, resp.StatusCode, err.Error())
	}
	if len(data) > maxResponse {
		return Response{}, a.fail(ErrDecode, resp.StatusCode, fmt.Sprintf("reply over %d MiB", maxResponse>>20))
	}
	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return Response{}, a.fail(ErrInvalid, resp.StatusCode, "redirect not followed")
	}
	if resp.StatusCode != http.StatusOK {
		return Response{}, a.fail(statusKind(resp.StatusCode), resp.StatusCode, apiErrorMessage(data))
	}
	if a.key != "" && holdsKey(data, a.key) {
		return Response{}, a.fail(ErrDecode, resp.StatusCode, "the reply contains the API key")
	}
	out, err := decodeResponse(data)
	if err != nil {
		return Response{}, a.fail(ErrDecode, resp.StatusCode, err.Error())
	}
	return out, nil
}

// holdsKey reports whether the reply body data holds key, as bytes or in
// any JSON string or object key once decoded (a JSON escape hides it from
// a byte search). An endpoint the host configured (a gateway, a proxy)
// could echo request headers into a reply, and a reply becomes Arv values.
func holdsKey(data []byte, key string) bool {
	if bytes.Contains(data, []byte(key)) {
		return true
	}
	var v any
	if json.Unmarshal(data, &v) != nil {
		return false // decodeResponse refuses it
	}
	var walk func(v any) bool
	walk = func(v any) bool {
		switch v := v.(type) {
		case string:
			return strings.Contains(v, key)
		case []any:
			for _, e := range v {
				if walk(e) {
					return true
				}
			}
		case map[string]any:
			for k, e := range v {
				if strings.Contains(k, key) || walk(e) {
					return true
				}
			}
		}
		return false
	}
	return walk(v)
}

// fail is an *Error whose message cannot hold the key, even if a server
// or a transport error echoed it.
func (a *Anthropic) fail(kind string, status int, msg string) *Error {
	if a.key != "" {
		msg = strings.ReplaceAll(msg, a.key, "[redacted]")
	}
	return &Error{Provider: "anthropic", Kind: kind, Status: status, Message: msg}
}

// statusKind is the Error kind for an HTTP status the API answered with.
func statusKind(status int) string {
	switch {
	case status == 401 || status == 403:
		return ErrAuth
	case status == 402:
		return ErrBilling
	case status == 404:
		return ErrNotFound
	case status == 429:
		return ErrRateLimit
	case status == 529:
		return ErrOverloaded
	case status >= 500:
		return ErrServer
	}
	return ErrInvalid
}

// apiErrorMessage is the message of an API error body
// ({"type":"error","error":{"type":...,"message":...}}), or "" if the body
// is not one.
func apiErrorMessage(data []byte) string {
	var e struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &e) != nil {
		return ""
	}
	if e.Error.Type != "" && e.Error.Message != "" {
		return e.Error.Type + ": " + e.Error.Message
	}
	return e.Error.Message
}
