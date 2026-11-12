package arv

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/delavalom/arvlang/arv/runtime"
)

// ScriptSHA256 is the hash of src a log's header records, so a host can
// warn that the script changed since the run was recorded.
func ScriptSHA256(src string) string {
	sum := sha256.Sum256([]byte(src))
	return hex.EncodeToString(sum[:])
}

// logSetup checks opts for a logged run, and for a replay or resume puts
// the header's limits in opts.Limits and returns its grants.
func logSetup(opts *Options) ([]runtime.Capability, error) {
	log := opts.Replay
	switch {
	case opts.Replay != nil && opts.Resume != nil:
		return nil, errors.New("arv: replay or resume a run, not both")
	case opts.Replay != nil && opts.Record != nil:
		return nil, errors.New("arv: a replay records nothing")
	case opts.Resume != nil && opts.Record == nil:
		return nil, errors.New("arv: resume appends to the log: set Record")
	case opts.Resume != nil:
		log = opts.Resume
	}
	if log == nil {
		return nil, nil
	}
	if opts.Replay != nil && len(opts.Caps) > 0 {
		return nil, errors.New("arv: a replay takes its grants from the log; pass none")
	}
	if opts.Limits != (runtime.Limits{}) {
		return nil, errors.New("arv: a replayed or resumed run takes its limits from the log; pass none")
	}
	h := log.Header
	opts.Limits = runtime.Limits{Timeout: time.Duration(h.Limits.TimeoutMS) * time.Millisecond,
		Steps: h.Limits.Steps, LiveTasks: h.Limits.LiveTasks, AllocBytes: h.Limits.AllocBytes}
	if err := opts.Limits.Validate(); err != nil {
		return nil, fmt.Errorf("arv: the log's header: %w", err)
	}
	caps := make([]runtime.Capability, len(h.Grants))
	for i, g := range h.Grants {
		c, err := runtime.Literal(g.Kind, g.Scope)
		if err != nil || c.Scope != g.Scope {
			return nil, fmt.Errorf("arv: the log's header grants %s, which is not a canonical capability", runtime.Capability{Kind: g.Kind, Scope: g.Scope})
		}
		caps[i] = c
	}
	return caps, nil
}

// validCaps validates and canonicalises the host's grants (runtime.New).
func validCaps(given []runtime.Capability) ([]runtime.Capability, error) {
	caps := make([]runtime.Capability, len(given))
	for i, c := range given {
		valid, err := runtime.New(c.Kind, c.Scope)
		if err != nil {
			return nil, fmt.Errorf("invalid capability %s: %w", c, err)
		}
		caps[i] = valid
	}
	return caps, nil
}

func sameGrants(a, b []runtime.Capability) bool {
	key := func(cs []runtime.Capability) []string {
		s := make([]string, len(cs))
		for i, c := range cs {
			s[i] = c.String()
		}
		slices.Sort(s)
		return slices.Compact(s)
	}
	return slices.Equal(key(a), key(b))
}

func grantList(caps []runtime.Capability) string {
	if len(caps) == 0 {
		return "none"
	}
	s := make([]string, len(caps))
	for i, c := range caps {
		s[i] = c.String()
	}
	return strings.Join(s, ", ")
}

// startLog makes the run's effect log, or nil for a run that keeps none.
func startLog(src string, opts Options, caps []runtime.Capability) (*runtime.EffectLog, error) {
	switch {
	case opts.Replay != nil:
		return runtime.NewReplayer(opts.Replay, opts.Out)
	case opts.Resume != nil:
		return runtime.NewResumer(opts.Resume, opts.Record, opts.Out, opts.RecordSync), nil
	case opts.Record == nil:
		return nil, nil
	}
	grants := make([]runtime.LogGrant, len(caps))
	for i, c := range caps {
		grants[i] = runtime.LogGrant{Kind: c.Kind, Scope: c.Scope}
	}
	cwd, _ := os.Getwd()
	h := runtime.Header{ScriptSHA256: ScriptSHA256(src), Grants: grants, Cwd: cwd,
		Started: time.Now().UTC().Format(time.RFC3339),
		Limits: runtime.LogLimits{TimeoutMS: ceilMS(opts.Limits.Timeout), Steps: opts.Limits.Steps,
			LiveTasks: opts.Limits.LiveTasks, AllocBytes: opts.Limits.AllocBytes}}
	return runtime.NewRecorder(opts.Record, h, opts.Out, opts.RecordSync)
}

// ceilMS rounds d up to whole milliseconds, so a replay's timeout is never
// shorter than the recording's.
func ceilMS(d time.Duration) int64 {
	ms := d.Milliseconds()
	if time.Duration(ms)*time.Millisecond < d {
		ms++
	}
	return ms
}
