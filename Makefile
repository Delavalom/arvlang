.PHONY: build install test test-walk e2e bench-test bench escape bench-cpu

# Where make build writes the binary. Only the command line moves it
# (make build BIN=dir, as the e2e suite does); an exported BIN does not.
BIN = bin

# The commit arv version names, with " (modified)" when the tree has
# changes. The go command stamps it in a plain clone but not in a git
# worktree, whose .git is a file, so make passes it at link time. Outside
# git it is empty and arv version falls back to what the go command knows.
COMMIT = $(shell git rev-parse HEAD 2>/dev/null)$(if $(shell git status --porcelain 2>/dev/null | head -n 1), (modified))
LDFLAGS = -X 'main.commit=$(COMMIT)'

# The command lives in cmd/arv (Phase 8.5), so go install names it arv.
build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN)/arv ./cmd/arv

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/arv

test:
	go test -race -timeout 60m ./...

e2e:
	go test -race -count=1 -timeout 40m ./e2e/

# Phase 8.4: the whole suite, e2e included, on the tree-walker, the
# reference engine (programs run as compiled code by default).
test-walk:
	go test -race -timeout 60m -tags arv_treewalk ./...
	ARV_E2E_TAGS=arv_treewalk go test -race -count=1 -timeout 40m ./e2e/

# The Go baseline of the Phase 5 gate is its own module, outside ./...
bench-test:
	cd bench/uptime-go && go test -race -timeout 10m ./...

# Phase 5 gate benchmark; output goes to bench/out/.
bench:
	bench/run.sh

# Phase 5 gate escape probe; output goes to bench/out/escape/.
escape:
	bench/escape/probe.sh

# Phase 8.4: the CPU-bound speed programs (arv/speed_bench_test.go).
bench-cpu:
	go test -run '^$$' -bench BenchmarkSpeed -benchmem -count 5 -timeout 30m ./arv/
