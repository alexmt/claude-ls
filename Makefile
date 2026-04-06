build:
	mkdir -p dist
	go build -o dist/claude-ls .

lint:
	golangci-lint run ./...

release-dry:
	goreleaser release --snapshot --clean
