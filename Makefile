.PHONY: build test lint clean

BINARY   := bin/lanmon
GOFLAGS  := -ldflags='-s -w'
GOFILES  := ./...

build:
	@mkdir -p bin
	go build $(GOFLAGS) -o $(BINARY) .

test:
	go test -race $(GOFILES)

vet:
	go vet $(GOFILES)

lint:
	@command -v golangci-lint >/dev/null 2>&1 && golangci-lint run $(GOFILES) || echo "golangci-lint not installed, skipping"

clean:
	rm -rf bin/
