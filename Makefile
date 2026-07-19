.PHONY: test check run

test:
	go test ./...

check:
	gofmt -w $$(find . -name '*.go')
	go vet ./...
	go test -race ./...

run:
	go run ./cmd/my-movie
