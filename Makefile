.PHONY: build run clean test vet build-linux build-darwin build-all

BINARY=gem2api

build:
	go build -o $(BINARY) ./cmd/server

run: build
	./$(BINARY)

clean:
	rm -f $(BINARY) $(BINARY)-linux $(BINARY)-darwin

build-linux:
	GOOS=linux GOARCH=amd64 go build -o $(BINARY)-linux ./cmd/server

build-darwin:
	GOOS=darwin GOARCH=arm64 go build -o $(BINARY)-darwin ./cmd/server

build-all: build-linux build-darwin

test:
	go test ./...

vet:
	go vet ./...
