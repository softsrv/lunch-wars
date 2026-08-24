BINARY := munchbot
CMD    := ./cmd/munchbot

.PHONY: dev build run test fmt vet tidy clean air-install

## dev: run the app with hot reloading via air (the app loads .env itself)
dev: air-install
	air -c .air.toml

## air-install: install air if it isn't already on PATH
air-install:
	@command -v air >/dev/null 2>&1 || go install github.com/air-verse/air@latest

## build: compile the binary to bin/munchbot
build:
	go build -o bin/$(BINARY) $(CMD)

## run: build and run the binary (the app loads .env itself)
run: build
	./bin/$(BINARY)

## test: run the test suite
test:
	go test ./...

## fmt: format all Go source files
fmt:
	gofmt -l -w .

## vet: run go vet
vet:
	go vet ./...

## tidy: tidy go.mod/go.sum
tidy:
	go mod tidy

## clean: remove build artifacts
clean:
	rm -rf bin tmp
