# Ryowatch (Go)

.PHONY: build run test clean

build:
	go build -o ryowatch .

run:
	go run .

test:
	go test ./...

clean:
	rm -f ryowatch
	go clean -cache