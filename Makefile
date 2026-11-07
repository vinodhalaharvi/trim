VERSION ?= dev

.PHONY: build test
build:
	go build -trimpath -ldflags="-s -w -X main.version=$(VERSION)" -o trim ./cmd/trim

test:
	go vet ./...
	go test ./...
