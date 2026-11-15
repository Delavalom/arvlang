package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Phase 7 runs the binary against a local stand-in for the Anthropic
// Messages API (ARV_ANTHROPIC_URL), one per test, so a test can count the
// requests that reached the provider and read what the model was sent.

// e2eKey is the API key every Phase 7 run gets. It must never appear in
// stdout, stderr or a request body: only in the x-api-key header.
const e2eKey = "sk-ant-e2e-SECRET-5551234567"

// apiReply is how the fake answers one request: status and body ($KEY in
// the body becomes the request's x-api-key, as a careless server might
// echo it), or, with hang, no answer until the client goes away.
type apiReply struct {
	status int
	body   string
	hang   bool
}

// llmAPI answers the n-th request with script[n] (the last entry again
// after that), or with respond(body) when that is set, and records every
// request's body and key. Each request also arrives at the gate "llm" of
// the test that made the fake, so a script can wait for a model call to be
// in flight (/wait?k=llm&n=N).
type llmAPI struct {
	*httptest.Server
	mu      sync.Mutex
	script  []apiReply
	respond func(body string) apiReply
	bodies  []string
	keys    []string
	gone    int           // hung requests the client abandoned
	wake    chan struct{} // closed and replaced when gone changes
	conns   *connTracker  // drain: see conntrack_test.go
}

func newLLMAPI(t *testing.T, respond func(string) apiReply, script ...apiReply) *llmAPI {
	t.Helper()
	if len(script) == 0 {
		script = []apiReply{okText("unused", 1, 1)}
	}
	a := &llmAPI{script: script, respond: respond, wake: make(chan struct{})}
	id := testID(t)
	a.Server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		a.mu.Lock()
		n := len(a.bodies)
		a.bodies = append(a.bodies, string(data))
		a.keys = append(a.keys, r.Header.Get("x-api-key"))
		reply := a.script[min(n, len(a.script)-1)]
		a.mu.Unlock()
		if a.respond != nil {
			reply = a.respond(string(data))
		}
		arrive(id, "llm")
		if reply.hang {
			select {
			case <-r.Context().Done():
				a.mu.Lock()
				a.gone++
				close(a.wake)
				a.wake = make(chan struct{})
				a.mu.Unlock()
			case <-time.After(2 * time.Minute):
			}
			return
		}
		w.WriteHeader(reply.status)
		io.WriteString(w, strings.ReplaceAll(reply.body, "$KEY", r.Header.Get("x-api-key")))
	}))
	a.conns = track(a.Server)
	a.Start()
	t.Cleanup(a.Close)
	return a
}

// drain waits until every request already written to the fake, and every
// request this test wrote to the shared server (tools reach it), has been
// handled: call it after a recording exits, before counting.
func (a *llmAPI) drain(t *testing.T) {
	t.Helper()
	if err := a.conns.settle(a.URL, "", drainLimit); err != nil {
		t.Fatal(err)
	}
	drainServer(t)
}

// requests returns a copy of every request body received so far.
func (a *llmAPI) requests() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.bodies...)
}

// waitGone waits until n hung requests have been abandoned by the client.
// The server notices a closed connection asynchronously, so this may wait
// after the process has exited.
func (a *llmAPI) waitGone(t *testing.T, n int, limit time.Duration) {
	t.Helper()
	deadline := time.NewTimer(limit)
	defer deadline.Stop()
	for {
		a.mu.Lock()
		gone, wake := a.gone, a.wake
		a.mu.Unlock()
		if gone >= n {
			return
		}
		select {
		case <-wake:
		case <-deadline.C:
			t.Fatalf("%d hung requests abandoned within %s, want %d", gone, limit, n)
		}
	}
}

func okReply(body string) apiReply { return apiReply{status: 200, body: body} }

// textBody is a final answer with the given usage.
func textBody(text string, in, out int) string {
	b, _ := json.Marshal(text)
	return fmt.Sprintf(`{"content":[{"type":"text","text":%s}],"stop_reason":"end_turn","usage":{"input_tokens":%d,"output_tokens":%d}}`, b, in, out)
}

func okText(text string, in, out int) apiReply { return okReply(textBody(text, in, out)) }

// use is one tool_use block; input is its JSON.
func use(id, name, input string) string {
	return fmt.Sprintf(`{"type":"tool_use","id":%q,"name":%q,"input":%s}`, id, name, input)
}

// okTools asks for the given tool_use blocks.
func okTools(in, out int, uses ...string) apiReply {
	return okReply(fmt.Sprintf(`{"content":[%s],"stop_reason":"tool_use","usage":{"input_tokens":%d,"output_tokens":%d}}`,
		strings.Join(uses, ","), in, out))
}

func apiError(status int, typ, msg string) apiReply {
	return apiReply{status: status, body: fmt.Sprintf(`{"type":"error","error":{"type":%q,"message":%q}}`, typ, msg)}
}

// llmCase is one inline program run with the fake provider.
type llmCase struct {
	name    string
	src     string   // {{SERVER}} and {{ID}} are expanded
	args    []string // flags before the file, e.g. "--allow", "llm=anthropic:*"
	script  []apiReply
	respond func(body string) apiReply // answers instead of script when set
	url     string                     // ARV_ANTHROPIC_URL instead of the fake's
	noKey   bool                       // run without ANTHROPIC_API_KEY
	path    string                     // where the program is written; "" for a new temp file
	dir     string                     // the run's working directory; "" for a new temp dir

	anyStdout bool // stdout is checked by the caller, not against stdout

	stdout string
	err    string // stderr must start "error: " and contain this; "" for success
	calls  int    // requests the provider must see; -1: not checked
	verify func(t *testing.T, res result, api *llmAPI)
}

// runLLM runs every case as a parallel subtest. Whatever the case, the key
// must not appear in stdout, stderr or any request body, and the binary
// must not crash.
func runLLM(t *testing.T, cases []llmCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runLLMCase(t, c)
		})
	}
}

func runLLMCase(t *testing.T, c llmCase) (result, *llmAPI) {
	t.Helper()
	api := newLLMAPI(t, c.respond, c.script...)
	path := c.path
	if path == "" {
		path = filepath.Join(t.TempDir(), "prog.arv")
	}
	mustWrite(t, path, expand(t, c.src, nil))
	url := api.URL + "/v1/messages"
	if c.url != "" {
		url = c.url
	}
	env := []string{"ARV_ANTHROPIC_URL=" + url}
	if !c.noKey {
		env = append(env, "ANTHROPIC_API_KEY="+e2eKey)
	}
	s := spec{
		args: append(append([]string{"run"}, c.args...), path), env: env, dir: c.dir, stdout: c.stdout,
		stderrNot: append([]string{e2eKey}, goCrash...), stdoutNot: []string{e2eKey}, timeout: 90 * time.Second,
	}
	if c.err != "" {
		s.code = 1
		s.stderrHas = []string{c.err}
	}
	res := run(t, s)
	if c.anyStdout {
		s.stdout = res.stdout
	}
	expect(t, s, res)
	bodies := api.requests()
	for i, b := range bodies {
		if strings.Contains(b, e2eKey) {
			t.Fatalf("request %d holds the API key:\n%s", i, b)
		}
	}
	api.mu.Lock()
	for i, k := range api.keys {
		if k != e2eKey && !c.noKey {
			t.Errorf("request %d carried x-api-key %q", i, k)
		}
	}
	api.mu.Unlock()
	if c.calls >= 0 && len(bodies) != c.calls {
		t.Fatalf("the provider saw %d requests, want %d\n%s", len(bodies), c.calls, res)
	}
	if c.verify != nil {
		c.verify(t, res, api)
	}
	return res, api
}

// sentHas checks that request i (0-based) holds every want.
func sentHas(i int, want ...string) func(*testing.T, result, *llmAPI) {
	return func(t *testing.T, res result, api *llmAPI) {
		t.Helper()
		bodies := api.requests()
		if i >= len(bodies) {
			t.Fatalf("no request %d (%d sent)", i, len(bodies))
		}
		for _, w := range want {
			if !strings.Contains(bodies[i], w) {
				t.Errorf("request %d lacks %s:\n%s", i, w, bodies[i])
			}
		}
	}
}

// sentNone checks that no request holds any of bad.
func sentNone(bad ...string) func(*testing.T, result, *llmAPI) {
	return func(t *testing.T, res result, api *llmAPI) {
		t.Helper()
		for i, b := range api.requests() {
			for _, w := range bad {
				if strings.Contains(b, w) {
					t.Errorf("request %d holds %q:\n%s", i, w, b)
				}
			}
		}
	}
}

// all runs every check in order.
func all(checks ...func(*testing.T, result, *llmAPI)) func(*testing.T, result, *llmAPI) {
	return func(t *testing.T, res result, api *llmAPI) {
		t.Helper()
		for _, c := range checks {
			c(t, res, api)
		}
	}
}

// gateArrivals is how many /arrive requests of this test hit gate k.
func gateArrivals(t *testing.T, k string) int {
	key := testID(t) + "/" + k
	gateMu.Lock()
	defer gateMu.Unlock()
	return gateN[key]
}

// wantArrivals checks arrivals at each gate.
func wantArrivals(want map[string]int) func(*testing.T, result, *llmAPI) {
	return func(t *testing.T, res result, api *llmAPI) {
		t.Helper()
		for k, n := range want {
			if got := gateArrivals(t, k); got != n {
				t.Errorf("gate %q: %d arrivals, want %d\n%s", k, got, n, res)
			}
		}
	}
}

// Models and grants the Phase 7 programs use.
const (
	sonnet   = `"anthropic:claude-sonnet-5-5"`
	haiku    = `"anthropic:claude-haiku-5-5"`
	allowLLM = "llm=anthropic:*"
)

// weatherTools are two tools that report each run to the gate server:
// weather (arrival "w-CITY") and notes (arrival "notes").
const weatherTools = `let tools = {
  "weather": fn(city) { http_get("{{SERVER}}/arrive?id={{ID}}&k=w-" + city); "sunny in " + city },
  "notes": fn(path) { http_get("{{SERVER}}/arrive?id={{ID}}&k=notes"); "secret notes" }
};
`
