package runtime

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

func TestRedactDenials(t *testing.T) {
	held := []Capability{{Net, "secret.internal"}, {FSRead, "/home/me/private"}, {LLM, "anthropic:*"}}
	cases := []struct {
		err  *DeniedError
		want string
	}{
		{&DeniedError{Task: "root/0", Want: Capability{Tool, "notes"}, Held: held}, `capability denied: tool("notes")`},
		{&DeniedError{Task: "root", Want: Capability{FSRead, "/etc/passwd"}, Held: held}, `capability denied: fs_read("/etc/passwd")`},
		{&DeniedError{Want: Capability{Env, "HOME"}, Held: held}, `capability denied: env("HOME")`},
		{&DeniedError{Task: "root/1", Want: Capability{Net, "evil.example:80"}, Held: held}, `capability denied: net`},
		{&DeniedError{Task: "root/1", Want: Capability{Net, "10.0.0.5:80"}, Held: held,
			Note: "metadata.example:80 reached a private address, which only an IP or CIDR grant covers"}, `capability denied: net`},
	}
	for _, c := range cases {
		for _, prefix := range []string{"", "http_get: ", "spawn in task root: "} {
			got := RedactDenials(prefix + c.err.Error())
			if got != prefix+c.want || got != prefix+c.err.Redacted() {
				t.Errorf("RedactDenials(%q) = %q, want %q", prefix+c.err.Error(), got, prefix+c.want)
			}
			for _, leak := range []string{"secret.internal", "/home/me/private", "anthropic:*", "holds", "root/", "10.0.0.5", "private address"} {
				if strings.Contains(got, leak) {
					t.Errorf("RedactDenials(%q) = %q leaks %q", prefix+c.err.Error(), got, leak)
				}
			}
		}
	}
	for in, want := range map[string]string{
		"no denial here":                   "no denial here",
		"capability denied: bogus(\"x\")":  "capability denied",
		"capability denied: net(\"x":       "capability denied",
		"x capability denied: tool(\"a\"]": "x capability denied",
	} {
		if got := RedactDenials(in); got != want {
			t.Errorf("RedactDenials(%q) = %q, want %q", in, got, want)
		}
	}
}

// Random denials, with scopes that contain quotes, brackets and the
// prefix itself: the redacted text never names a held grant.
func TestRedactDenialsRandom(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	pieces := []string{"a", `"`, "]", ")", " (holds: [", deniedPrefix, `\`, "\n", "é"}
	word := func() string {
		var b strings.Builder
		for n := r.Intn(6); n >= 0; n-- {
			b.WriteString(pieces[r.Intn(len(pieces))])
		}
		return b.String()
	}
	for i := 0; i < 3000; i++ {
		kinds := []Kind{Net, FSRead, FSWrite, Env, LLM, Tool}
		want := Capability{kinds[r.Intn(len(kinds))], word()}
		secret := fmt.Sprintf("SECRET%d", i)
		e := &DeniedError{Task: "root/" + secret, Want: want, Held: []Capability{{Env, secret}}}
		if r.Intn(2) == 0 {
			e.Note = secret + " reached a loopback address"
		}
		got := RedactDenials(e.Error())
		if got != e.Redacted() || strings.Contains(got, "SECRET") {
			t.Fatalf("RedactDenials(%q) = %q, want %q", e.Error(), got, e.Redacted())
		}
	}
}

func FuzzRedactDenials(f *testing.F) {
	f.Add("capability denied: net(\"a:80\") in task root (holds: [])")
	f.Add("x: capability denied: tool(\"t\") (holds: [tool(\"u\")])")
	f.Fuzz(func(t *testing.T, msg string) {
		got := RedactDenials(msg)
		i := strings.Index(msg, deniedPrefix)
		if i < 0 {
			if got != msg {
				t.Fatalf("no denial, but %q became %q", msg, got)
			}
			return
		}
		tail, ok := strings.CutPrefix(got, msg[:i])
		if !ok {
			t.Fatalf("RedactDenials(%q) = %q: the text before the denial changed", msg, got)
		}
		// The tail is a bare denial, or names exactly one request.
		if tail == "capability denied" || tail == deniedPrefix+"net" {
			return
		}
		if again := RedactDenials(tail); again != tail || !strings.HasSuffix(tail, ")") {
			t.Fatalf("RedactDenials(%q) = %q, not a redacted denial", msg, got)
		}
	})
}
