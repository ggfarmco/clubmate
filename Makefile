build:
	go build -o bin/ ./cmd/...

test:
	go test ./...

lint:
	go vet ./...

.PHONY: build test lint
