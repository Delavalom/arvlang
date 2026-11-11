.PHONY: build test e2e bench-test bench

build:
	go build -o bin/arv .

test:
	go test -race ./...

e2e:
	go test -race -count=1 ./e2e/

# The Go baseline of the Phase 5 gate is its own module, outside ./...
bench-test:
	cd bench/uptime-go && go test -race ./...

# Phase 5 gate benchmark; output goes to bench/out/.
bench:
	bench/run.sh
