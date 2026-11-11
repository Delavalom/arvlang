package main

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/delavalom/arvlang/arv"
	"github.com/delavalom/arvlang/arv/repl"
	"github.com/delavalom/arvlang/arv/runtime"
)

const usage = `usage:
  arv                    start the REPL (no capabilities)
  arv run [flags] FILE   run a program

flags:
  --allow kind=scope   grant a capability; repeat for more
                       kinds: net=HOST[:PORT] (HOST: name, *.domain, *, IP) or net=CIDR,
                              fs_read=PATH, fs_write=PATH, env=NAME,
                              llm=PROVIDER:MODEL (or PROVIDER:*, *), tool=NAME (or *)
  --timeout D          stop the run after D, e.g. 30s (default 10m; 0 for no limit)
  --steps N            evaluate at most N nodes in all (default: no limit)
  --tasks N            at most N tasks alive at once; one more is an error (default 1000)
  --alloc-mb N         allocate at most N MiB of Arv values in all (default: no limit)
  --tokens N           model tokens, input and output, of all calls (default: no limit)
  --usd-cents N        what model calls may cost in all, in US cents (default: no limit)
  --max-turns N        model calls one agent(...) may make (default 20)`

// defaultTimeout keeps a forgotten script from running forever.
const defaultTimeout = 10 * time.Minute

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Arv REPL. Ctrl-D to exit.")
		repl.Start(os.Stdin, os.Stdout)
		return
	}
	os.Exit(runCLI(os.Args[1:], os.Stdout, os.Stderr))
}

// runCLI implements every non-REPL command and returns the exit code.
func runCLI(args []string, stdout, stderr io.Writer) int {
	if args[0] != "run" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	opts, file, err := parseRun(args[1:], cwd)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 2
	}
	if file == "" {
		fmt.Fprintln(stderr, usage)
		return 2
	}
	src, err := os.ReadFile(file)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	opts.Out = stdout
	if _, err := arv.Run(string(src), opts); err != nil {
		var ae *arv.Error
		if errors.As(err, &ae) && ae.Pos != "" {
			fmt.Fprintf(stderr, "error: %s (at %s:%s)\n", ae.Msg, file, ae.Pos)
			return 1
		}
		var pe *arv.ParseError
		if errors.As(err, &pe) {
			fmt.Fprintf(stderr, "error: %s\n", pe.In(file))
			return 1
		}
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}

// parseRun reads run's flags, in any order, then the file name. file is ""
// when the arguments do not end in exactly one file.
func parseRun(args []string, cwd string) (opts arv.Options, file string, err error) {
	opts.Limits.Timeout = defaultTimeout
	for len(args) >= 2 && strings.HasPrefix(args[0], "--") {
		flag, val := args[0], args[1]
		args = args[2:]
		var n int64
		switch flag {
		case "--allow":
			c, err := runtime.Parse(val, cwd)
			if err != nil {
				return opts, "", err
			}
			opts.Caps = append(opts.Caps, c)
		case "--timeout":
			d, err := time.ParseDuration(val)
			if err != nil || d < 0 {
				return opts, "", fmt.Errorf("--timeout: want a duration like 30s or 10m, got %q", val)
			}
			opts.Limits.Timeout = d
		case "--steps":
			if n, err = positive(flag, val, math.MaxInt64); err != nil {
				return opts, "", err
			}
			opts.Limits.Steps = n
		case "--tasks":
			if n, err = positive(flag, val, math.MaxInt32); err != nil {
				return opts, "", err
			}
			opts.Limits.LiveTasks = int(n)
		case "--alloc-mb":
			if n, err = positive(flag, val, math.MaxInt64>>20); err != nil {
				return opts, "", err
			}
			opts.Limits.AllocBytes = n << 20
		case "--tokens":
			if n, err = positive(flag, val, math.MaxInt64); err != nil {
				return opts, "", err
			}
			opts.Limits.Tokens = n
		case "--usd-cents":
			if n, err = positive(flag, val, runtime.MaxUSDCents); err != nil {
				return opts, "", err
			}
			opts.Limits.USDCents = n
		case "--max-turns":
			if n, err = positive(flag, val, math.MaxInt32); err != nil {
				return opts, "", err
			}
			opts.Limits.MaxTurns = int(n)
		default:
			return opts, "", fmt.Errorf("unknown flag %q\n%s", flag, usage)
		}
	}
	if len(args) != 1 {
		return opts, "", nil
	}
	return opts, args[0], nil
}

// positive parses val as an integer from 1 to max.
func positive(flag, val string, max int64) (int64, error) {
	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil || n < 1 || n > max {
		return 0, fmt.Errorf("%s: want a whole number from 1 to %d, got %q", flag, max, val)
	}
	return n, nil
}
