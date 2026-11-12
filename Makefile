.PHONY: build test test-walk e2e bench-test bench escape bench-cpu

build:
	go build -o bin/arv .

test:
	go test -race ./...

e2e:
	go test -race -count=1 ./e2e/

# Phase 8.4: the whole suite, e2e included, on the tree-walker, the
# reference engine (programs run as compiled code by default).
test-walk:
	go test -race -tags arv_treewalk ./...
	ARV_E2E_TAGS=arv_treewalk go test -race -count=1 ./e2e/

# The Go baseline of the Phase 5 gate is its own module, outside ./...
bench-test:
	cd bench/uptime-go && go test -race ./...

# Phase 5 gate benchmark; output goes to bench/out/.
bench:
	bench/run.sh

# Phase 5 gate escape probe; output goes to bench/out/escape/.
escape:
	bench/escape/probe.sh

# Phase 8.4: the CPU-bound speed programs (arv/speed_bench_test.go).
bench-cpu:
	go test -run '^$$' -bench BenchmarkSpeed -benchmem -count 5 ./arv/
