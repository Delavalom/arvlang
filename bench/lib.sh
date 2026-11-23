# Shared helpers for the benchmark scripts (roadmap 03-benchmarks.md §B0).
# Source it from bash: . "$(dirname "$0")/lib.sh"
#
# The rules it enforces:
#   - Refuse to start when the 1-minute load average is above MAX_LOAD
#     (default 1.0 on Linux, 3.0 on macOS), and mark a run INVALID when the
#     load was above it at the end. The Phase 8.4 numbers were taken at
#     loads of 10 to 61; these rules would have refused them.
#   - Every result directory gets an env.txt naming the machine, the OS,
#     the Go version, the commit and the loads, next to the raw output.
#   - Results go under bench/results/, never only /tmp.

# load1 prints the 1-minute load average.
load1() {
	case "$(uname -s)" in
	Darwin) sysctl -n vm.loadavg | awk '{print $2}' ;;
	*) cut -d' ' -f1 /proc/loadavg ;;
	esac
}

# default_max_load prints the load limit for this OS.
default_max_load() {
	case "$(uname -s)" in
	Darwin) echo 3.0 ;;
	*) echo 1.0 ;;
	esac
}

# above LOAD LIMIT succeeds when LOAD > LIMIT.
above() {
	awk -v a="$1" -v b="$2" 'BEGIN { exit !(a > b) }'
}

# cpu_model prints the CPU model.
cpu_model() {
	case "$(uname -s)" in
	Darwin) sysctl -n machdep.cpu.brand_string ;;
	*) awk -F': ' '/model name/ {print $2; exit}' /proc/cpuinfo ;;
	esac
}

# check_load refuses to go on when the load is above MAX_LOAD.
check_load() {
	local l
	l=$(load1)
	if above "$l" "$MAX_LOAD"; then
		echo "bench: load average $l is above MAX_LOAD=$MAX_LOAD; not starting (03-benchmarks.md §B0)" >&2
		exit 3
	fi
	LOAD_BEFORE=$l
}

# results_dir NAME makes and prints bench/results/<date>-<host>-<commit>-NAME.
results_dir() {
	local repo=$1 name=$2 commit host d
	commit=$(git -C "$repo" rev-parse --short=12 HEAD 2>/dev/null || echo nogit)
	if [ -n "$(git -C "$repo" status --porcelain 2>/dev/null | head -n 1)" ]; then
		commit="$commit-dirty"
	fi
	host=${BENCH_HOST:-$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)}
	d=${BENCH_RESULTS:-$repo/bench/results}/$(date -u +%Y-%m-%d)-$host-$commit-$name
	mkdir -p "$d"
	(cd "$d" && pwd)
}

# write_env DIR writes DIR/env.txt (the "before" half).
write_env() {
	{
		echo "date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
		echo "os=$(uname -srm)"
		echo "cpu=$(cpu_model)"
		echo "ncpu=$(getconf _NPROCESSORS_ONLN)"
		echo "go=$(go version)"
		echo "commit=$(git -C "$REPO" rev-parse HEAD 2>/dev/null || echo nogit)"
		echo "dirty=$([ -n "$(git -C "$REPO" status --porcelain 2>/dev/null | head -n 1)" ] && echo yes || echo no)"
		echo "max_load=$MAX_LOAD"
		echo "load_before=$LOAD_BEFORE"
	} >"$1/env.txt"
}

# finish_env DIR records the load after the run, and fails (exit 3) and
# marks the run INVALID when it was above MAX_LOAD.
finish_env() {
	local l
	l=$(load1)
	echo "load_after=$l" >>"$1/env.txt"
	if above "$l" "$MAX_LOAD"; then
		echo "valid=no (load $l above $MAX_LOAD at the end)" >>"$1/env.txt"
		echo "bench: load average was $l at the end, above MAX_LOAD=$MAX_LOAD; run marked INVALID in $1/env.txt" >&2
		exit 3
	fi
	echo "valid=yes" >>"$1/env.txt"
}

# benchstat_bin prints a benchstat binary, installing it into the Go cache
# directory when it is not on PATH.
benchstat_bin() {
	if command -v benchstat >/dev/null 2>&1; then
		command -v benchstat
		return
	fi
	local gobin
	gobin=$(go env GOCACHE)/arv-bench-bin
	if [ ! -x "$gobin/benchstat" ]; then
		GOBIN=$gobin go install golang.org/x/perf/cmd/benchstat@latest >&2
	fi
	echo "$gobin/benchstat"
}
