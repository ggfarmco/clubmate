build:
	go build -o bin/ ./cmd/...
	go build -o bin/plugins/terminals/terminals ./plugins/terminals
	cp plugins/terminals/plugin.json bin/plugins/terminals/

run-core: build
	bin/clubmate-core

test:
	go test ./...

lint:
	go vet ./...

.PHONY: build run-core test lint
