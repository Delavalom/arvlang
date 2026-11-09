.PHONY: build test e2e

build:
	go build -o bin/arv .

test:
	go test -race ./...

e2e:
	go test -race -count=1 ./e2e/
