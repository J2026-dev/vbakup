.PHONY: build test clean
build:
	go build -o bin/vbakup-controller ./cmd/controller
	go build -o bin/vbakup-agent ./cmd/agent
test:
	go test ./...
clean:
	rm -rf bin
