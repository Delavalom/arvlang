package e2e

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// Phase 10: http(req) and parse_json through the real binary, against a
// server of their own (a request's method, headers and body matter here).
//
//	/echo           201 "METHOD|X-A|Authorization|body"
//	/json           200 {"name":"arv","n":[1,2,3]}
//	/away           302 to http://localhost:PORT/echo (another host name)
//	/away307        307 to http://localhost:PORT/echo
type p10Server struct {
	*httptest.Server
	port string
	mu   sync.Mutex
	hits map[string]int // path -> requests
}

func newP10Server(t *testing.T) *p10Server {
	t.Helper()
	s := &p10Server{hits: map[string]int{}}
	mux := http.NewServeMux()
	count := func(r *http.Request) {
		s.mu.Lock()
		s.hits[r.URL.Path]++
		s.mu.Unlock()
	}
	mux.HandleFunc("/echo", func(w http.ResponseWriter, r *http.Request) {
		count(r)
		b, _ := io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
		fmt.Fprintf(w, "%s|%s|%s|%s", r.Method, r.Header.Get("X-A"), r.Header.Get("Authorization"), b)
	})
	mux.HandleFunc("/json", func(w http.ResponseWriter, r *http.Request) {
		count(r)
		io.WriteString(w, `{"name":"arv","n":[1,2,3]}`)
	})
	mux.HandleFunc("/away", func(w http.ResponseWriter, r *http.Request) {
		count(r)
		http.Redirect(w, r, "http://localhost:"+s.port+"/echo", http.StatusFound)
	})
	mux.HandleFunc("/away307", func(w http.ResponseWriter, r *http.Request) {
		count(r)
		http.Redirect(w, r, "http://localhost:"+s.port+"/echo", http.StatusTemporaryRedirect)
	})
	s.Server = httptest.NewServer(mux)
	s.port = strconv.Itoa(s.Listener.Addr().(*net.TCPAddr).Port)
	t.Cleanup(s.Close)
	return s
}

func (s *p10Server) hitsOf(path string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hits[path]
}

// p10Run writes src (with {{URL}} the server's) into a temp dir and runs
// the binary there with args before the script.
func p10Run(t *testing.T, s *p10Server, src string, args ...string) (result, string) {
	t.Helper()
	dir := t.TempDir()
	script := filepath.Join(dir, "main.arv")
	mustWrite(t, script, strings.ReplaceAll(src, "{{URL}}", s.URL))
	return logRun(t, dir, append(append([]string{"run"}, args...), script)...), dir
}

const p10Post = `let r = http({"url": "{{URL}}/echo", "method": "POST", "headers": {"X-A": "1", "Authorization": "Bearer t"}, "body": "hello"});
puts(r["status"]);
puts(r["body"]);`

// I3: without a net grant, http sends nothing and the run fails with the
// denial.
func TestP10HTTPDeniedWithoutGrant(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	res, _ := p10Run(t, s, p10Post)
	if res.code != 1 || res.stdout != "" || !strings.Contains(res.stderr, `capability denied: net("127.0.0.1:`+s.port+`")`) {
		t.Fatalf("%s", res)
	}
	if s.hitsOf("/echo") != 0 {
		t.Fatal("the request reached the server")
	}
}

// A POST with headers and a body.
func TestP10HTTPPost(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	res, _ := p10Run(t, s, p10Post, "--allow", "net=127.0.0.1")
	if res.code != 0 || res.stdout != "201\nPOST|1|Bearer t|hello\n" || res.stderr != "" {
		t.Fatalf("%s", res)
	}
}

// I3: a redirect to an ungranted host is refused before it is followed.
func TestP10HTTPRedirectToUngrantedHost(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	res, _ := p10Run(t, s, `puts(http({"url": "{{URL}}/away", "method": "POST", "body": "b"})["body"])`, "--allow", "net=127.0.0.1")
	if res.code != 1 || !strings.Contains(res.stderr, `capability denied: net("localhost:`+s.port+`")`) {
		t.Fatalf("%s", res)
	}
	if s.hitsOf("/away") != 1 || s.hitsOf("/echo") != 0 {
		t.Fatalf("hits %v", s.hits)
	}
}

// A 307 to another host would send the body there: refused, even with
// both hosts granted.
func TestP10HTTPRedirectKeepsTheBodyHome(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	res, _ := p10Run(t, s, `puts(http({"url": "{{URL}}/away307", "method": "POST", "body": "secret"})["body"])`,
		"--allow", "net=127.0.0.1", "--allow", "net=localhost")
	if res.code != 1 || !strings.Contains(res.stderr, "a 307 redirect to localhost:"+s.port+" would send the request body to another host") {
		t.Fatalf("%s", res)
	}
	if s.hitsOf("/echo") != 0 {
		t.Fatal("the body reached the other host")
	}
}

// I3: a name grant never reaches an internal address: localhost resolves
// to loopback, which needs an IP grant.
func TestP10HTTPInternalAddressThroughANameGrant(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	src := `puts(http({"url": "http://localhost:` + s.port + `/echo", "method": "PUT", "body": "b"})["body"])`
	res, _ := p10Run(t, s, src, "--allow", "net=localhost")
	if res.code != 1 || !strings.Contains(res.stderr, "capability denied: net(") || !strings.Contains(res.stderr, "reached a loopback address") {
		t.Fatalf("%s", res)
	}
	if s.hitsOf("/echo") != 0 {
		t.Fatal("the request reached the server")
	}
}

// I7: record, then replay with no grant: the same output byte for byte
// and no request. A replay of an edited request diverges, naming both.
func TestP10HTTPRecordReplay(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	src := p10Post + "\n" + `let j = parse_json(http({"url": "{{URL}}/json"})["body"]); puts(j["name"]); puts(j["n"][2]);`
	dir := t.TempDir()
	script := filepath.Join(dir, "main.arv")
	mustWrite(t, script, strings.ReplaceAll(src, "{{URL}}", s.URL))
	log := filepath.Join(dir, "run.jsonl")
	rec := logRun(t, dir, "run", "--record", log, "--allow", "net=127.0.0.1", script)
	if rec.code != 0 || rec.stdout != "201\nPOST|1|Bearer t|hello\narv\n3\n" || s.hitsOf("/echo") != 1 {
		t.Fatalf("record\n%s", rec)
	}
	rep := logRun(t, dir, "run", "--replay", log, script)
	if rep.code != 0 || rep.stdout != rec.stdout || rep.stderr != "replay: grants: net(\"127.0.0.1\")\n" {
		t.Fatalf("replay\n%s", rep)
	}
	if s.hitsOf("/echo") != 1 || s.hitsOf("/json") != 1 {
		t.Fatalf("the replay made requests: %v", s.hits)
	}
	mustWrite(t, script, strings.ReplaceAll(strings.Replace(src, `"body": "hello"`, `"body": "bye"`, 1), "{{URL}}", s.URL))
	div := logRun(t, dir, "run", "--replay", log, script)
	if div.code != 1 || div.stdout != "" || !strings.Contains(div.stderr, "replay diverged at task root, effect 1: the log has http(") ||
		!strings.Contains(div.stderr, `["body","hello"]`) || !strings.Contains(div.stderr, `["body","bye"]`) {
		t.Fatalf("divergence\n%s", div)
	}
	if s.hitsOf("/echo") != 1 {
		t.Fatal("the divergent replay went live")
	}
}

// arv check reports http's host from a literal request, and a note when
// it cannot know it.
func TestP10Check(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	script := filepath.Join(dir, "main.arv")
	mustWrite(t, script, `http({"url": "https://api.example.com/v1", "method": "POST"});
let f = fn(u) { http({"url": u}) };
parse_json("[1]");
`)
	res := logRun(t, dir, "check", script)
	if want := "--allow net=api.example.com:443\n# http at 2:17: host not statically known\n"; res.code != 0 || res.stdout != want {
		t.Fatalf("want %q\n%s", want, res)
	}
}

// parse_json errors name the problem and where; a float is refused.
func TestP10ParseJSONErrors(t *testing.T) {
	t.Parallel()
	s := newP10Server(t)
	res, _ := p10Run(t, s, `puts(parse_json(json([1, 2]))[1]); parse_json("[1, 2");`)
	if res.code != 1 || res.stdout != "2\n" || !strings.Contains(res.stderr, "error: parse_json: unexpected end of JSON input") {
		t.Fatalf("%s", res)
	}
	res, _ = p10Run(t, s, `parse_json("1.5")`)
	if res.code != 1 || !strings.Contains(res.stderr, "parse_json: number 1.5 at line 1, column 1 is not an integer, and Arv has no floats yet") {
		t.Fatalf("%s", res)
	}
}
