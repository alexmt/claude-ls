build:
	go build -o dist/claude-ls .

lint:
	golangci-lint run ./...
